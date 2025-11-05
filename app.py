from flask import Flask, render_template, request, redirect, url_for, abort, make_response, session, json
import docker
import os
import time
import logging
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.interval import IntervalTrigger
import requests
import threading
import random
import secrets
from datetime import datetime
from flask_wtf.csrf import CSRFProtect

# Set up logging
logging.basicConfig(level=logging.DEBUG)
logger = logging.getLogger(__name__)

# Generate secure key
app_secret_key = secrets.token_hex(24)
print("Generated Secret Key:", app_secret_key)
logger.debug(f"Generated Secret Key: {app_secret_key}")

# Flask app
app = Flask(__name__)
app.secret_key = app_secret_key
csrf = CSRFProtect(app)

@app.context_processor
def inject_now():
    return {'now': datetime.utcnow}

# Docker client
docker_host = os.getenv('DOCKER_HOST', 'unix:///var/run/docker.sock')
logger.debug(f"Docker host URL: {docker_host}")
client = docker.DockerClient(base_url=docker_host)

try:
    version = client.version()
    logger.debug(f"Docker version: {version}")
except docker.errors.DockerException as e:
    logger.error(f"Docker exception: {e}")

# Container name
CONTAINER_NAME = os.getenv('DOCKER_CONTAINER_NAME', 'my_container')

# File path for time persistence
FILE_PATH = '/hostmem/gamecontroller-palworld-time_remaining.json'
last_status = None
last_status_update = 0
broadcasted_last = 0
time_lock = threading.Lock()

# === DISCORD API CONFIG ===
DISCORD_BOT_TOKEN = os.getenv('DISCORD_BOT_TOKEN')
DISCORD_GUILD_ID = os.getenv('DISCORD_GUILD_ID')
DISCORD_CHANNEL_ID = os.getenv('DISCORD_CHANNEL_ID')
DISCORD_FALLBACK_URL = os.getenv('DISCORD_FALLBACK_URL', 'https://discord.gg/XXXXXINVITENOTFOUNDXXXXXX')

# Invite cache
_invite_cache = {'url': None, 'last_update': 0}
CACHE_TTL = 3600  # 1 hour

def load_time_remaining():
    if os.path.exists(FILE_PATH):
        with open(FILE_PATH, 'r') as file:
            data = json.load(file)
            time_remaining = data.get('time_remaining', 900)
            if time_remaining == 0:
                time_remaining = 900
            return time_remaining
    return 900

time_remaining = load_time_remaining()

def save_time_remaining(time_remaining):
    with open(FILE_PATH, 'w') as file:
        json.dump({'time_remaining': time_remaining}, file)

def get_discord_invite():
    """Generate fresh invite via Discord API with caching. Accepts 200/201."""
    current_time = time.time()
    if _invite_cache['url'] and (current_time - _invite_cache['last_update']) < CACHE_TTL:
        logger.debug("Using cached Discord invite")
        return _invite_cache['url']

    if not all([DISCORD_BOT_TOKEN, DISCORD_GUILD_ID, DISCORD_CHANNEL_ID]):
        logger.warning("Missing Discord bot config; using fallback")
        return DISCORD_FALLBACK_URL

    try:
        headers = {
            'Authorization': f'Bot {DISCORD_BOT_TOKEN}',
            'Content-Type': 'application/json'
        }
        payload = {
            'max_age': 86400,
            'max_uses': 0,
            'temporary': False,
            'unique': True
        }
        response = requests.post(
            f'https://discord.com/api/v10/channels/{DISCORD_CHANNEL_ID}/invites',
            headers=headers,
            json=payload,
            timeout=10
        )

        if response.status_code in (200, 201):
            invite_data = response.json()
            invite_code = invite_data.get('code')
            if invite_code:
                new_url = f'https://discord.gg/{invite_code}'
                _invite_cache['url'] = new_url
                _invite_cache['last_update'] = current_time
                logger.info(f"Generated/refreshed Discord invite: {new_url}")
                return new_url
            else:
                logger.warning("Discord API response missing 'code' field")
        else:
            logger.error(f"Discord API error {response.status_code}: {response.text}")

    except Exception as e:
        logger.error(f"Failed to generate Discord invite: {e}")

    return DISCORD_FALLBACK_URL

@app.route('/stop', methods=['POST'])
def stop_container():
    if request.remote_addr not in ['127.0.0.1', '::1']:
        logger.warning(f"Unauthorized stop from {request.remote_addr}")
        abort(403)

    global time_remaining, broadcasted_last
    with time_lock:
        try:
            container = client.containers.get(CONTAINER_NAME)
            if container.status == 'running':
                logger.debug(f"Running backup in '{CONTAINER_NAME}'")
                result = container.exec_run('backup')
                if result.exit_code != 0:
                    logger.error(f"Backup failed: {result.output.decode()}")
                container.stop()
                logger.debug(f"Container '{CONTAINER_NAME}' stopped")
        except docker.errors.NotFound:
            logger.error("Container not found")
        except Exception as e:
            logger.error(f"Stop error: {e}")
        finally:
            time_remaining = 0
            broadcasted_last = 0
            save_time_remaining(time_remaining)

    return "OK"

def get_container_status():
    global last_status, last_status_update
    now = time.time()
    if now - last_status_update >= 30:
        try:
            container = client.containers.get(CONTAINER_NAME)
            last_status = container.status
            last_status_update = now
        except docker.errors.NotFound:
            last_status = "unknown"
    return last_status

@app.route('/')
def index():
    global time_remaining
    status = get_container_status()
    docker_container_name = os.getenv('DOCKER_CONTAINER_NAME', 'DefaultContainerName')
    discord_url = get_discord_invite()

    logger.debug(f"Index: {docker_container_name}, {status}, {time_remaining}s, Discord: {discord_url}")
    response = make_response(render_template(
        'index.html',
        docker_container_name=docker_container_name,
        status=status,
        time_remaining=time_remaining,
        discord_url=discord_url
    ))
    response.headers['Cache-Control'] = 'no-store'
    return response

@app.route('/start', methods=['POST'])
def start_container():
    global time_remaining, broadcasted_last
    captcha_answer = request.form.get('captcha_answer')
    
    if captcha_answer and int(captcha_answer) == session.get('captcha_answer'):
        with time_lock:
            try:
                container = client.containers.get(CONTAINER_NAME)
                if container.status != 'running':
                    container.start()
                    logger.debug(f"Container '{CONTAINER_NAME}' started")
                    time_remaining = max(time_remaining, 900)
                    time_remaining += 14400
                    save_time_remaining(time_remaining)
                    broadcasted_last = time.time()
            except docker.errors.NotFound:
                logger.error("Container not found")
            except Exception as e:
                logger.error(f"Start error: {e}")
        return redirect(url_for('index'))
    else:
        return redirect(url_for('captcha_error', origin='start_container'))

def update_timer():
    global time_remaining
    with time_lock:
        if time_remaining > 0:
            time_remaining -= 30
            if time_remaining < 0:
                time_remaining = 0
            save_time_remaining(time_remaining)
            if time_remaining == 0:
                stop_container()
    return "OK"

# ==================
# CAPTCHA FUNCTIONS
# ==================

def generate_captcha(language='en'):
    operation = random.choice(['+', '-'])
    num1 = random.randint(100, 199)
    num2 = random.randint(0, 99) if operation == '-' else random.randint(1, 99)
    answer = num1 + num2 if operation == '+' else num1 - num2

    # === Number to words (German & English) ===
    def number_to_words(n, lang):
        ones = {
            'de': ['null', 'eins', 'zwei', 'drei', 'vier', 'fünf', 'sechs', 'sieben', 'acht', 'neun', 'zehn',
                   'elf', 'zwölf', 'dreizehn', 'vierzehn', 'fünfzehn', 'sechzehn', 'siebzehn', 'achtzehn', 'neunzehn'],
            'en': ['zero', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten',
                   'eleven', 'twelve', 'thirteen', 'fourteen', 'fifteen', 'sixteen', 'seventeen', 'eighteen', 'nineteen']
        }
        tens = {
            'de': ['', '', 'zwanzig', 'dreißig', 'vierzig', 'fünfzig', 'sechzig', 'siebzig', 'achtzig', 'neunzig'],
            'en': ['', '', 'twenty', 'thirty', 'forty', 'fifty', 'sixty', 'seventy', 'eighty', 'ninety']
        }
        if n < 20:
            return ones[lang][n]
        elif n < 100:
            ten, one = divmod(n, 10)
            connector = 'und' if one and lang == 'de' else '-' if one and lang == 'en' else ''
            return f"{tens[lang][ten]}{connector}{ones[lang][one] if one else ''}".strip()
        else:
            h, r = divmod(n, 100)
            h_str = 'einhundert' if h == 1 and lang == 'de' else ones[lang][h] + ('hundert' if lang == 'de' else 'hundred')
            return f"{h_str}{number_to_words(r, lang) if r else ''}"

    num1_words = number_to_words(num1, language)
    num2_words = number_to_words(num2, language)

    # === Themes ===
    themes = {
        'de': [
            ['Abenteurer', 'Schätze', 'im dichten Wald'],
            ['Zauberer', 'Zauberstäbe', 'auf dem magischen Berg'],
            ['Ritter', 'Schwerter', 'in der alten Burg'],
            ['Entdecker', 'Karten', 'am Ufer des Meeres'],
            ['Jäger', 'Pfeile', 'in der Wildnis'],
            ['Alchemist', 'Tränke', 'im Labor'],
            ['Piratenkapitän', 'Goldmünzen', 'auf hoher See'],
            ['Drachenreiter', 'Schuppen', 'in den Wolken'],
            ['Gärtner', 'Blumen', 'im verzauberten Garten'],
            ['Koch', 'Zutaten', 'in der Küche']
        ],
        'en': [
            ['adventurer', 'treasures', 'in the dense forest'],
            ['wizard', 'wands', 'on the magical mountain'],
            ['knight', 'swords', 'in the ancient castle'],
            ['explorer', 'maps', 'by the seaside'],
            ['hunter', 'arrows', 'in the wilderness'],
            ['alchemist', 'potions', 'in the laboratory'],
            ['pirate captain', 'gold coins', 'on the high seas'],
            ['dragon rider', 'scales', 'in the clouds'],
            ['gardener', 'flowers', 'in the enchanted garden'],
            ['cook', 'ingredients', 'in the kitchen']
        ]
    }
    theme = random.choice(themes[language])
    actor = theme[0].capitalize() if language == 'en' else theme[0]
    item = theme[1]
    setting = theme[2]

    # === Intro phrases ===
    intros = {
        'de': [
            f"Stell dir vor, in einem epischen Abenteuer: Der {actor} {setting}",
            f"In einer fernen Welt: Der {actor} {setting}",
            f"In einer mystischen Geschichte: Der {actor} {setting}"
        ],
        'en': [
            f"Imagine in an epic adventure: The {actor} {setting}",
            f"In a distant world: The {actor} {setting}",
            f"In a mystical story: The {actor} {setting}"
        ]
    }
    intro = random.choice(intros[language])

    # === Addition Templates (20+ variations) ===
    addition_templates = {
        'de': [
            f"{intro} beginnt mit {num1_words} {item}. Plötzlich findet er {num2_words} weitere {item}.",
            f"{intro} hat {num1_words} {item} bei sich. Dann entdeckt er {num2_words} weitere {item} in einer Truhe.",
            f"{intro} zählt {num1_words} {item}. Plötzlich erscheinen {num2_words} neue {item}.",
            f"{intro} trägt {num1_words} {item}. Am Wegesrand findet er {num2_words} zusätzliche {item}.",
            f"{intro} beginnt mit {num1_words} {item}. Ein Händler schenkt ihm {num2_words} weitere {item}.",
            f"{intro} besitzt {num1_words} {item}. Dann fällt {num2_words} {item} vom Himmel.",
            f"{intro} sammelt {num1_words} {item}. In einer Höhle entdeckt er {num2_words} weitere {item}.",
            f"{intro} hat {num1_words} {item}. Ein Freund gibt ihm {num2_words} zusätzliche {item}.",
            f"{intro} startet mit {num1_words} {item}. Am Ende des Pfads findet er {num2_words} neue {item}.",
            f"{intro} zählt {num1_words} {item}. Dann wachsen {num2_words} neue {item} aus dem Boden.",
        ],
        'en': [
            f"{intro} starts with {num1_words} {item}. Suddenly, he finds {num2_words} more {item}.",
            f"{intro} has {num1_words} {item} with him. Then he discovers {num2_words} more {item} in a chest.",
            f"{intro} counts {num1_words} {item}. Suddenly, {num2_words} new {item} appear.",
            f"{intro} carries {num1_words} {item}. By the roadside, he finds {num2_words} additional {item}.",
            f"{intro} begins with {num1_words} {item}. A merchant gives him {num2_words} more {item}.",
            f"{intro} owns {num1_words} {item}. Then {num2_words} {item} fall from the sky.",
            f"{intro} collects {num1_words} {item}. In a cave, he discovers {num2_words} more {item}.",
            f"{intro} has {num1_words} {item}. A friend gives him {num2_words} extra {item}.",
            f"{intro} starts with {num1_words} {item}. At the end of the path, he finds {num2_words} new {item}.",
            f"{intro} counts {num1_words} {item}. Then {num2_words} new {item} grow from the ground.",
        ]
    }

    # === Subtraction Templates (20+ variations) ===
    subtraction_templates = {
        'de': [
            f"{intro} beginnt mit {num1_words} {item}. Doch dann verschwinden {num2_words} dieser {item} im Nebel.",
            f"{intro} hat {num1_words} {item}. Plötzlich lösen sich {num2_words} {item} in Rauch auf.",
            f"{intro} besitzt {num1_words} {item}. Ein Dieb stiehlt {num2_words} davon.",
            f"{intro} zählt {num1_words} {item}. Dann fallen {num2_words} {item} in einen Abgrund.",
            f"{intro} trägt {num1_words} {item}. {num2_words} davon zerbrechen bei einem Sturm.",
            f"{intro} sammelt {num1_words} {item}. Ein Drache verbrennt {num2_words} davon.",
            f"{intro} hat {num1_words} {item}. {num2_words} werden von einem Fluch zerstört.",
            f"{intro} beginnt mit {num1_words} {item}. Ein starker Wind trägt {num2_words} davon.",
            f"{intro} besitzt {num1_words} {item}. {num2_words} versinken im Treibsand.",
            f"{intro} zählt {num1_words} {item}. Dann explodieren {num2_words} davon.",
        ],
        'en': [
            f"{intro} starts with {num1_words} {item}. But then {num2_words} of these {item} disappear in the mist.",
            f"{intro} has {num1_words} {item}. Suddenly, {num2_words} {item} dissolve into smoke.",
            f"{intro} owns {num1_words} {item}. A thief steals {num2_words} of them.",
            f"{intro} counts {num1_words} {item}. Then {num2_words} {item} fall into a chasm.",
            f"{intro} carries {num1_words} {item}. {num2_words} of them break in a storm.",
            f"{intro} collects {num1_words} {item}. A dragon burns {num2_words} of them.",
            f"{intro} has {num1_words} {item}. {num2_words} are destroyed by a curse.",
            f"{intro} begins with {num1_words} {item}. A strong wind carries {num2_words} away.",
            f"{intro} owns {num1_words} {item}. {num2_words} sink into quicksand.",
            f"{intro} counts {num1_words} {item}. Then {num2_words} of them explode.",
        ]
    }

    # === Final Question ===
    if operation == '+':
        base = random.choice(addition_templates[language])
        question = f"{base} Wie viele {item} hat er jetzt?" if language == 'de' else f"{base} How many {item} does he have now?"
    else:
        base = random.choice(subtraction_templates[language])
        question = f"{base} Wie viele {item} bleiben übrig?" if language == 'de' else f"{base} How many {item} are left?"

    session['captcha_answer'] = answer
    session['captcha_language'] = language
    return question

##################    
###CAPTCHA END####
##################

@app.route('/captcha')
def captcha():
    default_lang = request.accept_languages.best_match(['de', 'en'], default='en')
    lang = request.args.get('lang') or session.get('captcha_language') or default_lang
    if lang not in ['de', 'en']:
        lang = 'en'
    session['captcha_language'] = lang
    question = generate_captcha(lang)
    discord_url = get_discord_invite()
    return render_template('captcha.html', question=question, discord_url=discord_url, language=lang)

@app.route('/captcha_start')
def captcha_start():
    default_lang = request.accept_languages.best_match(['de', 'en'], default='en')
    lang = request.args.get('lang') or session.get('captcha_language') or default_lang
    if lang not in ['de', 'en']:
        lang = 'en'
    session['captcha_language'] = lang
    question = generate_captcha(lang)
    discord_url = get_discord_invite()
    return render_template('captcha_start.html', question=question, discord_url=discord_url, language=lang)

@app.route('/captcha_error')
def captcha_error():
    origin = request.args.get('origin', 'add_time')
    discord_url = get_discord_invite()
    return render_template(
        'captcha_error.html',
        discord_url=discord_url,
        retry_target=origin,
        language=session.get('captcha_language', 'en')
    )

@app.route('/add_time', methods=['POST'])
def add_time():
    global time_remaining
    captcha_answer = request.form.get('captcha_answer')
    
    if captcha_answer and int(captcha_answer) == session.get('captcha_answer'):
        status = get_container_status()
        if status == 'running':
            with time_lock:
                time_remaining += 14400
                save_time_remaining(time_remaining)
                logger.debug(f"Added 4 hours, new time: {time_remaining}s")
            return redirect(url_for('index'))
        else:
            logger.warning("Add time attempted on non-running container")
            return redirect(url_for('index'))
    else:
        return redirect(url_for('captcha_error', origin='add_time'))

@csrf.exempt
@app.route('/trigger_timer', methods=['POST'])
def trigger_timer():
    return update_timer()

def trigger_timer_task():
    try:
        requests.post('http://localhost:5000/trigger_timer', timeout=5)
    except:
        pass

def broadcast_server_link():
    message = "to start this server visit https://pal.wowcraft.pw/"
    try:
        container = client.containers.get(CONTAINER_NAME)
        if container.status == 'running':
            result = container.exec_run(f'rcon-cli "Broadcast {message}"')
            if result.exit_code != 0:
                logger.error(f"Broadcast failed: {result.output.decode()}")
    except:
        pass
        
def broadcast_pal_link():
    message = "https://pal.wowcraft.pw"
    try:
        container = client.containers.get(CONTAINER_NAME)
        if container.status == 'running':
            result = container.exec_run(f'rcon-cli "Broadcast {message}"')
            if result.exit_code != 0:
                logger.error(f"Pal-link broadcast failed: {result.output.decode()}")
    except Exception as e:
        logger.debug(f"Pal-link broadcast skipped: {e}")

def refresh_discord_invite():
    get_discord_invite()
    
def check_and_extend_on_players():
    try:
        container = client.containers.get(CONTAINER_NAME)
        if container.status != 'running':
            logger.debug("Player check skipped: Container not running")
            return

        result = container.exec_run(
            'rcon-cli "ShowPlayers"',
            stdout=True,
            stderr=True,
            demux=False,
            socket_timeout=5
        )

        if result.exit_code != 0:
            err = result.output.decode('utf-8', errors='ignore').strip()
            if 'i/o timeout' in err.lower() or 'timeout' in err.lower():
                logger.debug("ShowPlayers timed out – assuming no players or busy server")
                return
            else:
                logger.error(f"ShowPlayers failed (exit {result.exit_code}): {err}")
                return

        output = result.output.decode('utf-8', errors='ignore').strip()
        if not output:
            logger.debug("ShowPlayers returned empty output")
            return

        player_lines = [line for line in output.splitlines() if line.strip()]
        num_players = len(player_lines) - 1 if len(player_lines) > 0 and ',' in player_lines[0] else len(player_lines)

        if num_players > 0:
            with time_lock:
                global time_remaining
                time_remaining += 120
                save_time_remaining(time_remaining)
                logger.debug(f"Extended time by 5 min due to {num_players} players. New time: {time_remaining}s")
        else:
            logger.debug("No players connected; no time extension")

    except docker.errors.NotFound:
        logger.error("Player check skipped: Container not found")
    except Exception as e:
        logger.error(f"Player check error: {e}")

# === SCHEDULER ===
scheduler = BackgroundScheduler()
scheduler.add_job(func=broadcast_server_link, trigger=IntervalTrigger(minutes=30), id='broadcast_job')
scheduler.add_job(func=broadcast_pal_link, trigger=IntervalTrigger(minutes=68), id='broadcast_pal_link_job')
scheduler.add_job(func=trigger_timer_task, trigger=IntervalTrigger(seconds=30), id='timer_job')
scheduler.add_job(func=refresh_discord_invite, trigger=IntervalTrigger(minutes=30), id='discord_refresh')
scheduler.add_job(func=check_and_extend_on_players, trigger=IntervalTrigger(seconds=600), id='player_extend_job')
scheduler.start()

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000, debug=True)