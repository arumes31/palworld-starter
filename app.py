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

# ==================== CONFIG & LOGGING ====================
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = Flask(__name__)
app.secret_key = secrets.token_hex(24)
csrf = CSRFProtect(app)

@app.context_processor
def inject_now():
    return {'now': datetime.utcnow()}

# ==================== DOCKER CLIENT ====================
docker_host = os.getenv('DOCKER_HOST', 'unix:///var/run/docker.sock')
client = docker.DockerClient(base_url=docker_host)

CONTAINER_NAME = os.getenv('DOCKER_CONTAINER_NAME', 'my_container')
FILE_PATH = '/hostmem/gamecontroller-palworld-time_remaining.json'

# ==================== DISCORD CONFIG ====================
DISCORD_BOT_TOKEN = os.getenv('DISCORD_BOT_TOKEN')
DISCORD_GUILD_ID = os.getenv('DISCORD_GUILD_ID')
DISCORD_CHANNEL_ID = os.getenv('DISCORD_CHANNEL_ID')
DISCORD_FALLBACK_URL = os.getenv('DISCORD_FALLBACK_URL', 'https://discord.gg/XXXXXINVITENOTFOUNDXXXXXX')

_invite_cache = {'url': None, 'last_update': 0}
CACHE_TTL = 3600

# ==================== GLOBAL STATE ====================
time_lock = threading.Lock()
last_status = None
last_status_update = 0

def load_time_remaining():
    """Load saved time. NEVER auto-reset to 900 when 0!"""
    if os.path.exists(FILE_PATH):
        try:
            with open(FILE_PATH, 'r') as f:
                data = json.load(f)
                remaining = data.get('time_remaining', 900)
                return max(0, int(remaining))
        except Exception as e:
            logger.error(f"Failed to load time file: {e}")
    return 900  # only on very first start ever

def save_time_remaining(remaining):
    try:
        with open(FILE_PATH, 'w') as f:
            json.dump({'time_remaining': remaining}, f)
    except Exception as e:
        logger.error(f"Failed to save time file: {e}")

time_remaining = load_time_remaining()

# ==================== DISCORD INVITE ====================
def get_discord_invite():
    now = time.time()
    if _invite_cache['url'] and (now - _invite_cache['last_update']) < CACHE_TTL:
        return _invite_cache['url']

    if not all([DISCORD_BOT_TOKEN, DISCORD_GUILD_ID, DISCORD_CHANNEL_ID]):
        return DISCORD_FALLBACK_URL

    try:
        headers = {'Authorization': f'Bot {DISCORD_BOT_TOKEN}', 'Content-Type': 'application/json'}
        payload = {'max_age': 86400, 'max_uses': 0, 'temporary': False, 'unique': True}
        r = requests.post(
            f'https://discord.com/api/v10/channels/{DISCORD_CHANNEL_ID}/invites',
            headers=headers, json=payload, timeout=10
        )
        if r.status_code in (200, 201):
            code = r.json().get('code')
            if code:
                url = f'https://discord.gg/{code}'
                _invite_cache.update({'url': url, 'last_update': now})
                return url
    except Exception as e:
        logger.error(f"Discord invite failed: {e}")

    return DISCORD_FALLBACK_URL

# ==================== SERVER STATE HELPERS ====================
def get_container_status():
    global last_status, last_status_update
    now = time.time()
    if now - last_status_update > 30:
        try:
            container = client.containers.get(CONTAINER_NAME)
            last_status = container.status
            last_status_update = now
        except docker.errors.NotFound:
            last_status = "exited"
        except Exception as e:
            logger.error(f"Status check error: {e}")
            last_status = "unknown"
    return last_status

def is_server_paused():
    """Detect if Palworld is in AUTO PAUSE state (very reliable with itzg image)"""
    try:
        container = client.containers.get(CONTAINER_NAME)
        logs = container.logs(tail=40).decode('utf-8', errors='ignore')
        lines = [line.strip() for line in logs.splitlines() if line.strip()]
        paused = False
        for line in reversed(lines):
            if '[AUTO PAUSE] Paused' in line:
                paused = True
            elif any(x in line for x in ['Wakeup!!!', 'Resumed by', 'Player connected', 'Player disconnected']):
                return False  # was paused but woke up
        return paused
    except:
        return False

def get_player_count():
    """Use REST API instead of RCON → doesn't wake paused server"""
    try:
        r = requests.get('http://localhost:8212/v1/api/players', timeout=4)
        if r.status_code == 200:
            return len(r.json().get('players', []))
    except:
        pass
    return 0

# ==================== ROUTES ====================
@app.route('/')
def index():
    status = get_container_status()
    discord_url = get_discord_invite()
    return render_template(
        'index.html',
        docker_container_name=os.getenv('DOCKER_CONTAINER_NAME', 'Palworld Server'),
        status=status,
        time_remaining=time_remaining,
        discord_url=discord_url
    )

@app.route('/stop', methods=['POST'])
def stop_container():
    if request.remote_addr not in ['127.0.0.1', '::1']:
        abort(403)

    global time_remaining
    with time_lock:
        try:
            container = client.containers.get(CONTAINER_NAME)
            if container.status == 'running':
                container.exec_run('backup', detach=False)
                container.stop()
                logger.info("Container stopped due to time expiry or manual stop")
        except Exception as e:
            logger.error(f"Stop failed: {e}")
        finally:
            time_remaining = 0
            save_time_remaining(0)

    return "OK"

@app.route('/start', methods=['POST'])
def start_container():
    global time_remaining
    if not request.form.get('captcha_answer') or int(request.form.get('captcha_answer')) != session.get('captcha_answer'):
        return redirect(url_for('captcha_error', origin='start_container'))

    with time_lock:
        try:
            container = client.containers.get(CONTAINER_NAME)
            if container.status != 'running':
                container.start()
                time_remaining = max(time_remaining, 900) + 14400  # 15 min grace + 4 hours
                save_time_remaining(time_remaining)
                logger.info("Server started + 4 hours added")
        except Exception as e:
            logger.error(f"Start failed: {e}")

    return redirect(url_for('index'))

@app.route('/add_time', methods=['POST'])
def add_time():
    global time_remaining
    if not request.form.get('captcha_answer') or int(request.form.get('captcha_answer')) != session.get('captcha_answer'):
        return redirect(url_for('captcha_error', origin='add_time'))

    if get_container_status() == 'running':
        with time_lock:
            time_remaining += 43200
            save_time_remaining(time_remaining)
            logger.info(f"+12 hours added, now {time_remaining//3600}h")
        return redirect(url_for('index'))

    return redirect(url_for('index'))

# ==================== TIMER & AUTO-EXTEND ====================
def update_timer():
    global time_remaining
    with time_lock:
        if time_remaining > 0:
            time_remaining -= 30
            if time_remaining <= 0:
                time_remaining = 0
                save_time_remaining(0)
                if get_container_status() == 'running':
                    logger.info("TIME EXPIRED → stopping container")
                    requests.post('http://localhost:5000/stop', timeout=5)
        save_time_remaining(time_remaining)

def extend_if_players():
    if is_server_paused() or get_container_status() != 'running':
        return

    count = get_player_count()
    if count > 0:
        with time_lock:
            global time_remaining
            was = time_remaining
            time_remaining += 300  # +5 minutes per player seen
            time_remaining = min(time_remaining, 172800)  # max 48h
            if time_remaining != was:
                save_time_remaining(time_remaining)
                logger.info(f"Players online ({count}) → +5 min (now {time_remaining//3600}h)")

def safe_broadcast(message):
    if is_server_paused() or get_container_status() != 'running':
        logger.debug("Broadcast skipped – server paused or not running")
        return
    try:
        container = client.containers.get(CONTAINER_NAME)
        result = container.exec_run(f'rcon-cli "Broadcast {message}"')
        if result.exit_code != 0:
            logger.warning(f"RCON broadcast failed: {result.output.decode()}")
    except Exception as e:
        logger.debug(f"RCON unavailable: {e}")
        
##Backup
def auto_backup_if_running_and_not_paused():
    """Runs every 15 minutes – backs up only if container is running AND game is not paused"""
    if get_container_status() != 'running':
        logger.debug("Backup skipped: container not running")
        return

    if is_server_paused():
        logger.debug("Backup skipped: server is auto-paused (no players)")
        return

    try:
        container = client.containers.get(CONTAINER_NAME)
        logger.info("Running scheduled backup...")
        result = container.exec_run('backup', detach=False)
        if result.exit_code == 0:
            logger.info("Backup completed successfully")
        else:
            logger.warning(f"Backup failed: {result.output.decode('utf-8', errors='ignore').strip()}")
    except Exception as e:
        logger.error(f"Backup error: {e}")

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
            if one == 0:
                return tens[lang][ten]
            else:
                if lang == 'de':
                    return f"{ones[lang][one]}und{tens[lang][ten]}"
                else:
                    return f"{tens[lang][ten]}-{ones[lang][one]}"

        else:
            h, r = divmod(n, 100)
            if lang == 'de':
                hundred = 'einhundert' if h == 1 else f"{ones[lang][h]}hundert"
            else:
                hundred = 'one hundred' if h == 1 else f"{ones[lang][h]} hundred"
            return hundred + (number_to_words(r, lang) if r else '')

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

def trigger_timer_task():
    try:
        requests.post('http://localhost:5000/trigger_timer', timeout=5)
    except:
        pass
        
def refresh_discord_invite():
    get_discord_invite()
    
def broadcast_start_message():
    safe_broadcast("to start this server visit https://pal.wowcraft.pw/")

def broadcast_server_url():
    safe_broadcast("https://pal.wowcraft.pw")

def trigger_timer_task():
    try:
        requests.post('http://localhost:5000/trigger_timer', timeout=5)
    except Exception as e:
        logger.debug(f"Timer trigger failed (normal when container stopped): {e}")

# ==================== SCHEDULER JOBS ====================
scheduler = BackgroundScheduler()

scheduler.add_job(func=broadcast_start_message, trigger=IntervalTrigger(minutes=30),  id='broadcast_link')
scheduler.add_job(func=broadcast_server_url, trigger=IntervalTrigger(minutes=68),  id='broadcast_url')
scheduler.add_job(func=trigger_timer_task, trigger=IntervalTrigger(seconds=30),  id='timer')
scheduler.add_job(func=get_discord_invite, trigger=IntervalTrigger(minutes=30), id='discord_refresh')
scheduler.add_job(func=extend_if_players, trigger=IntervalTrigger(seconds=300), id='player_extend')
scheduler.add_job(func=auto_backup_if_running_and_not_paused, trigger=IntervalTrigger(minutes=15), name='auto_backup', max_instances=1, coalesce=True)

scheduler.start()

@csrf.exempt
@app.route('/trigger_timer', methods=['POST'])
def trigger_timer():
    update_timer()
    return "OK"

# ==================== RUN ====================
if __name__ == '__main__':
    logger.info("Palworld Free Server Controller started")
    app.run(host='0.0.0.0', port=5000, debug=True)