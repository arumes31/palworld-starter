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
            'unique': True  # Reuse same invite if identical
        }
        response = requests.post(
            f'https://discord.com/api/v10/channels/{DISCORD_CHANNEL_ID}/invites',
            headers=headers,
            json=payload,
            timeout=10
        )

        # Accept both 200 and 201 as success
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
    discord_url = get_discord_invite()  # Dynamic!

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
            except docker.errors.NotFound:
                pass
            time_remaining = 18000
            broadcasted_last = 0
            save_time_remaining(time_remaining)
        return redirect(url_for('index'))
    else:
        return redirect(url_for('captcha_error'))

@app.route('/update_timer')
def update_timer():
    if request.remote_addr not in ['127.0.0.1', '::1']:
        abort(403)
    global time_remaining, broadcasted_last
    with time_lock:
        if time_remaining > 0:
            time_remaining -= 30
            save_time_remaining(time_remaining)
            if time_remaining <= 0:
                time_remaining = 0
    return "OK"


##################    
###CAPTCHA START####
##################

def number_to_words(number):
    """Konvertiert eine Zahl in Worte und verwendet bis zu 10 verschiedene Formulierungen."""
    number_words = {
        1: ["eins"],
        2: ["zwei"],
        3: ["drei"],
        4: ["vier"],
        5: ["fünf"],
        6: ["sechs"],
        7: ["sieben"],
        8: ["acht"],
        9: ["neun"],
        10: ["zehn"],
        11: ["elf"],
        12: ["zwölf"],
        13: ["dreizehn"],
        14: ["vierzehn"],
        15: ["fünfzehn"],
        16: ["sechzehn"],
        17: ["siebzehn"],
        18: ["achtzehn"],
        19: ["neunzehn"],
        20: ["zwanzig"],
        21: ["einundzwanzig"],
        22: ["zweiundzwanzig"],
        23: ["dreiundzwanzig"],
        24: ["vierundzwanzig"],
        25: ["fünfundzwanzig"],
        26: ["sechsundzwanzig"],
        27: ["siebenundzwanzig"],
        28: ["achtundzwanzig"],
        29: ["neunundzwanzig"],
        30: ["dreißig"]
    }

    return random.choice(number_words[number])

def generate_captcha():
    # Randomly choose between addition and subtraction
    operation = random.choice(['+', '-'])
    
    # Generate random numbers
    num1 = random.randint(1, 20)
    num2 = random.randint(1, 20)
    
    # Ensure the result is positive for subtraction
    if operation == '-':
        if num1 < num2:
            num1, num2 = num2, num1
        answer = num1 - num2
    else:
        answer = num1 + num2
    
    session['captcha_answer'] = answer
    
    # Convert numbers to words using the number_to_words function
    num1_words = number_to_words(num1)
    num2_words = number_to_words(num2)
    
    # Themes and creatures for more randomization
    intros = [
        "Im Herzen des alten Waldes von",
        "Tief in den schimmernden Reichen von",
        "Inmitten der zeitlosen Ruinen von",
        "Unter dem wachsamen Blick der himmlischen Sterne über",
        "Im ewigen Mondlicht von",
        "In der mystischen Weite von",
        "In den verborgenen Tiefen von",
        "Unter den heiligen Böden von",
        "Am Rande des sagenhaften Landes von",
        "Im geheimen Heiligtum von"
    ]

    themes = [
        ("Zauberer", "alte Zauberbücher", "eine staubige Bibliothek", "ein schimmerndes Portal"),
        ("Drache", "mondbeschienene Diamanten", "ein verstecktes Versteck", "ein Meteoritenregen"),
        ("Feenkönigin", "verzauberte Lilien", "mystische Wiesen", "eine magische Quelle"),
        ("Elf", "magische Pilze", "ein mondbeschienener Hain", "ein mystisches Ereignis"),
        ("Himmlisches Wesen", "funkelnde Sterne", "ihr kosmisches Reich", "ein himmlisches Ereignis"),
        ("Zauberer", "verzauberte Tränke", "ein geheimes Gewölbe", "eine Beschwörung"),
        ("Mystisches Wesen", "verzauberte Federn", "ein versteckter Hain", "eine ätherische Brise"),
        ("Schutzengel", "himmlische Schriftrollen", "ein heiliges Archiv", "eine dunkle Macht"),
        ("Nekromant", "alte Runen", "ein verfluchtes Grab", "eine unheimliche Erscheinung"),
        ("Gestaltwandler", "mysteriöse Artefakte", "eine labyrinthartige Höhle", "eine rätselhafte Verwandlung"),
        ("Meerjungfrau", "Korallenschätze", "eine Unterwassergrotte", "eine mondbeschienene Flut"),
        ("Ninja", "alte Schriftrollen", "ein versteckter Tempel", "ein heimlicher Ansatz"),
        ("Golem", "elementare Kristalle", "eine vergessene Festung", "ein mystisches Erwachen"),
        ("Dschinn", "alte Lampen", "eine Wüstenoase", "ein plötzlicher Wirbelwind"),
        ("Vampirfürst", "Blutsteinrelikte", "ein schattiges Schloss", "ein Mitternachtsmahl"),
        ("Werwolf", "Silbertalismane", "ein dunkler Wald", "ein Vollmond"),
        ("Naturgeist", "glühende Flora", "ein ruhiger Hain", "ein saisonaler Wechsel"),
        ("Phönix", "feurige Federn", "ein alter Scheiterhaufen", "eine Sonnenaufgangswiedergeburt"),
        ("Zeitreisender", "Chrono-Gadgets", "eine futuristische Stadtlandschaft", "ein Zeitparadoxon"),
        ("Druide", "alte Runensteine", "ein heiliger Hain", "ein saisonales Ritual"),
        ("Gargoyle", "Steinstatuen", "eine gotische Kathedrale", "ein dunkler, stürmischer Abend"),
        ("Hexe", "mystische Amulette", "eine verzauberte Hütte", "eine Geisterstunde"),
        ("Greif", "legendäre Artefakte", "ein verlassener Berg", "ein stürmischer Aufstieg"),
        ("Schattenmagier", "verfluchte Schmuckstücke", "eine geheime Zuflucht", "ein stiller, tödlicher Wind"),
        ("Samurai", "antike Katanas", "ein Kirschblütenhain", "eine ehrenvolle Herausforderung"),
        ("Dämonenjäger", "verzauberte Waffen", "eine düstere Stadt", "eine nächtliche Jagd"),
        ("Kobold", "goldene Münzen", "eine versteckte Höhle", "ein trickreicher Streich"),
        ("Zentaur", "endlose Graslandschaften", "eine wilde Jagd", "ein Stammesritual"),
        ("Eisdämon", "gefrorene Kristalle", "ein eisiger Palast", "ein Sturm aus Schnee und Frost"),
        ("Dunkler Ritter", "zerbrochene Rüstungen", "eine verlassene Schlachtfeld", "eine verlorene Ehre"),
        ("Lichtbringer", "strahlende Relikte", "ein heiliger Tempel", "ein Hoffnungsschimmer"),
        ("Hexenmeister", "dämonische Grimoire", "ein verfluchter Turm", "ein Pakt mit der Finsternis"),
        ("Zirkus der Schatten", "verzauberte Masken", "ein wandernder Zirkus", "eine unheimliche Vorstellung"),
        ("Runenmeister", "leuchtende Runen", "eine verborgene Akademie", "ein verlorenes Wissen"),
        ("Meereskönig", "versunkene Schätze", "eine mystische Insel", "ein Sturm auf hoher See"),
        ("Piratenkapitän", "vergrabene Schätze", "ein Piratenschiff", "eine geheimnisvolle Karte"),
        ("Schlangenpriester", "giftige Relikte", "ein Tempel im Dschungel", "ein tödliches Ritual"),
        ("Alchemist", "mystische Elixiere", "ein geheimer Laborraum", "eine wundersame Verwandlung")
    ]
    
    theme = random.choice(themes)
    intro = random.choice(intros)
    
    # Addition und Subtraktionsfragen mit den Themen und Intros
    addition_questions = [
        f"{intro} {theme[2]}, entdeckt {theme[0]} {num1_words} {theme[1]} verstreut in den alten Ruinen. Während der {theme[0]} sein Fundstück bewundert, erscheinen {num2_words} weitere {theme[1]}, die mit magischem Licht schimmern. Wie viele {theme[1]} hat der {theme[0]} jetzt?",
        f"{intro} {theme[2]}, {theme[0].capitalize()} entdeckt {num1_words} {theme[1]} verborgen unter dem mystischen Blätterdach. Plötzlich materialisieren sich {num2_words} weitere {theme[1]} aus dem verzauberten Nebel. Wie viele {theme[1]} gibt es insgesamt?",
        f"{intro} {theme[2]}, mitten in der mystischen Landschaft, stößt {theme[0]} auf {num1_words} {theme[1]} in einem versteckten Hain. Zu seiner Überraschung erscheinen {num2_words} weitere {theme[1]}, die durch die alten Bäume flattern. Wie viele {theme[1]} sind jetzt insgesamt da?",
        f"{intro} {theme[2]}, beginnt der {theme[0]} seine Suche mit {num1_words} {theme[1]}. Während er weiter erkundet, werden {num2_words} weitere {theme[1]} entdeckt, die in einem geheimen Hain versteckt sind. Wie viele {theme[1]} hat der {theme[0]} insgesamt?",
        f"{intro} {theme[2]}, im Herzen des verzauberten Reiches, findet der {theme[0]} {num1_words} {theme[1]} verstreut auf den mystischen Böden. Plötzlich erscheinen {num2_words} weitere {theme[1]}, die im Mondlicht tanzen. Wie viele {theme[1]} gibt es jetzt insgesamt?",
        f"{intro} {theme[2]}, während der {theme[0]} die magische Landschaft durchquert, findet er {num1_words} {theme[1]} unter einem Baldachin von leuchtenden Lichtern. Aus dem Nichts flattern {num2_words} weitere {theme[1]} von den verzauberten Ästen herunter. Wie viele {theme[1]} gibt es jetzt?",
        f"{intro} {theme[2]}, sammelt der {theme[0]} {num1_words} {theme[1]}, während er den mystischen Wald durchquert. Zu seiner Überraschung erscheinen {num2_words} weitere {theme[1]}, die mit alter Magie schimmern. Wie viele {theme[1]} hat der {theme[0]} jetzt?",
        f"{intro} {theme[2]}, während ihrer Reise durch die verzauberten Wälder findet {theme[0]} {num1_words} {theme[1]} unter dem alten Laub verborgen. Während sie ihre Entdeckung bestaunen, erscheinen {num2_words} weitere {theme[1]}, die im Mondlicht funkeln. Wie viele {theme[1]} gibt es insgesamt?",
        f"{intro} {theme[2]}, entdeckt der {theme[0]} {num1_words} {theme[1]} während der Erkundung eines versteckten Tals. Plötzlich erscheinen {num2_words} weitere {theme[1]}, die wie magische Kugeln umherwirbeln. Wie viele {theme[1]} hat der {theme[0]} insgesamt?",
        # New additional questions
        f"{intro} {theme[2]}, während seiner Reise durch die alten Berge, entdeckt der {theme[0]} {num1_words} {theme[1]} in einem geheimen Tal. Plötzlich erscheinen {num2_words} weitere {theme[1]} aus dem Nebel. Wie viele {theme[1]} gibt es jetzt?",
        f"{intro} {theme[2]}, im Dämmerlicht eines mystischen Waldes, findet {theme[0]} {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} weitere {theme[1]}, die im Wind tanzen. Wie viele {theme[1]} gibt es nun?",
        f"{intro} {theme[2]}, während der {theme[0]} die verzauberten Hügel erklimmt, findet er {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} neue {theme[1]}, die mit Glanz durch die Luft fliegen. Wie viele {theme[1]} sind es jetzt?",
        f"{intro} {theme[2]}, im tiefen Wald, stößt der {theme[0]} auf {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} weitere {theme[1]} mit magischem Glanz. Wie viele {theme[1]} gibt es nun?",
        f"{intro} {theme[2]}, entdeckt {theme[0]} {num1_words} {theme[1]} an einem verborgenen Ort. Doch plötzlich erscheinen {num2_words} weitere {theme[1]}, die im Morgenlicht glitzern. Wie viele {theme[1]} sind es jetzt?",
        f"{intro} {theme[2]}, während der {theme[0]} durch ein verzaubertes Tal geht, findet er {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} weitere {theme[1]}, die in der Luft schweben. Wie viele {theme[1]} sind es nun?",
        f"{intro} {theme[2]}, mitten im mystischen Garten, stößt der {theme[0]} auf {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} weitere {theme[1]}, die im sanften Wind fliegen. Wie viele {theme[1]} gibt es jetzt?",
        f"{intro} {theme[2]}, als der {theme[0]} den alten Tempel erkundet, findet er {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} weitere {theme[1]}, die mit goldenen Flügeln schimmern. Wie viele {theme[1]} gibt es nun?",
        f"{intro} {theme[2]}, entdeckt {theme[0]} {num1_words} {theme[1]} in einer alten Höhle. Doch dann erscheinen {num2_words} weitere {theme[1]}, die mit magischem Licht glänzen. Wie viele {theme[1]} gibt es jetzt?",
        f"{intro} {theme[2]}, während der {theme[0]} durch das mystische Tal wandert, entdeckt er {num1_words} {theme[1]}. Doch dann erscheinen {num2_words} weitere {theme[1]}, die im Sonnenlicht leuchten. Wie viele {theme[1]} gibt es insgesamt?",
    ]

    subtraction_questions = [
        f"{intro} {theme[2]}, beginnt der {theme[0]} mit {num1_words} {theme[1]}. Nach einer plötzlichen Begegnung mit einer mystischen Kraft werden {num2_words} dieser {theme[1]} weggenommen. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, in einem mystischen Land beginnt der {theme[0]} mit {num1_words} {theme[1]}. Nach einer magischen Störung verschwinden {num2_words} {theme[1]} im Äther. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, findet der {theme[0]} {num1_words} {theme[1]} auf seiner Reise. Doch {num2_words} dieser {theme[1]} gehen aufgrund eines unerwarteten Ereignisses verloren. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, während er ein verborgenes Reich erkundet, beginnt der {theme[0]} mit {num1_words} {theme[1]}. Während eines magischen Aufruhrs werden {num2_words} dieser {theme[1]} fortgetragen. Wie viele {theme[1]} hat der {theme[0]} jetzt?",
        f"{intro} {theme[2]}, hat der {theme[0]} {num1_words} {theme[1]}. Nach einer plötzlichen Störung in den mystischen Energien gehen {num2_words} {theme[1]} verloren. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, beginnt ein {theme[0]} mit {num1_words} {theme[1]}. Doch während eines unvorhergesehenen Ereignisses werden {num2_words} dieser {theme[1]} von einer geheimnisvollen Kraft genommen. Wie viele {theme[1]} bleiben in ihrem Besitz?",
        f"{intro} {theme[2]}, im Herzen des alten Landes hat der {theme[0]} {num1_words} {theme[1]}. Nach einem magischen Missgeschick verschwinden {num2_words} {theme[1]}. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, begegnet der {theme[0]} {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} gehen durch ein plötzliches magisches Ereignis verloren. Wie viele {theme[1]} bleiben dem {theme[0]}?",
        f"{intro} {theme[2]}, während er die mystischen Wälder erkundet, beginnt der {theme[0]} mit {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} gehen durch ein magisches Phänomen verloren. Wie viele {theme[1]} behält der {theme[0]}?",
        # New additional questions
        f"{intro} {theme[2]}, auf seiner Reise durch ein mystisches Tal, findet der {theme[0]} {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} verschwinden im Nebel. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, im Land der Geheimnisse, findet der {theme[0]} {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} verschwinden in der Dunkelheit. Wie viele {theme[1]} bleiben zurück?",
        f"{intro} {theme[2]}, während {theme[0]} durch die weiten Ebenen wandert, beginnt er mit {num1_words} {theme[1]}. Doch dann verschwinden {num2_words} dieser {theme[1]} in den Wind. Wie viele {theme[1]} sind noch übrig?",
        f"{intro} {theme[2]}, im verzauberten Dschungel, beginnt der {theme[0]} mit {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} verschwinden, als ein mystischer Sturm aufzieht. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, als der {theme[0]} den alten Tempel betritt, findet er {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} verschwinden durch einen magischen Zauber. Wie viele {theme[1]} bleiben zurück?",
        f"{intro} {theme[2]}, bei der Erkundung einer alten Stadt findet der {theme[0]} {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} verschwinden, als ein mystischer Lichtstrahl sie erfasst. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, während {theme[0]} einen verborgenen Pfad erkundet, findet er {num1_words} {theme[1]}. Doch dann verschwinden {num2_words} dieser {theme[1]} im Nebel. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, entdeckt {theme[0]} {num1_words} {theme[1]} in einem geheimen Garten. Doch {num2_words} dieser {theme[1]} verschwinden, als ein magischer Wind sie erfasst. Wie viele {theme[1]} bleiben übrig?",
        f"{intro} {theme[2]}, während der {theme[0]} durch die ewigen Berge wandert, beginnt er mit {num1_words} {theme[1]}. Doch {num2_words} dieser {theme[1]} verschwinden, als ein magischer Sturm aufzieht. Wie viele {theme[1]} bleiben übrig?",
    ]

    if operation == '+':
        question = random.choice(addition_questions)
    else:
        question = random.choice(subtraction_questions)
    
    return question

##################    
###CAPTCHA END####
##################

@app.route('/captcha')
def captcha():
    question = generate_captcha()
    return render_template('captcha.html', question=question)

@app.route('/captcha_start')
def captcha_start():
    question = generate_captcha()
    return render_template('captcha_start.html', question=question)

@app.route('/captcha_error')
def captcha_error():
    return render_template('captcha_error.html')

@app.route('/add_time', methods=['POST'])
def add_time():
    global time_remaining
    captcha_answer = request.form.get('captcha_answer')
    
    if captcha_answer and int(captcha_answer) == session.get('captcha_answer'):
        status = get_container_status()
        if status == 'running':
            with time_lock:
                time_remaining += 14400  # 4 hours in seconds
                save_time_remaining(time_remaining)
                logger.debug(f"Added 4 hours, new time: {time_remaining}s")
            return redirect(url_for('index'))
        else:
            # Optional: Handle non-running case (e.g., redirect or error)
            logger.warning("Add time attempted on non-running container")
            return redirect(url_for('index'))  # Or to an error page
    else:
        return redirect(url_for('captcha_error'))

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

        # Run rcon-cli "ShowPlayers"
        result = container.exec_run('rcon-cli "ShowPlayers"')
        if result.exit_code != 0:
            logger.error(f"ShowPlayers failed: {result.output.decode()}")
            return

        # Parse output: Assume CSV format with header. Count lines >1 for players.
        output = result.output.decode().strip()
        player_lines = output.splitlines()
        num_players = len(player_lines) - 1 if player_lines else 0  # Subtract header

        if num_players > 0:
            with time_lock:
                global time_remaining
                time_remaining += 300  # Add 5 minutes
                save_time_remaining(time_remaining)
                logger.debug(f"Extended time by 5 min due to {num_players} players connected. New time: {time_remaining}s")
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
scheduler.add_job(func=check_and_extend_on_players, trigger=IntervalTrigger(seconds=300), id='player_extend_job')
scheduler.start()

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000, debug=True)