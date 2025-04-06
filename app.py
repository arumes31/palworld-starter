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

# Set up logging for verbose debugging
logging.basicConfig(level=logging.DEBUG)
logger = logging.getLogger(__name__)

# Generate a secure random string
app_secret_key = secrets.token_hex(24)  # Generates a 48-character hexadecimal string
print("Generated Secret Key:", app_secret_key)
logger.debug(f"Generated Secret Key successfully: {app_secret_key}")  # Print the key to copy it

# Primary Flask app (main app)
app = Flask(__name__)
app.secret_key = app_secret_key  # Use the generated key here

# Define the Docker client with the correct URL
docker_host = os.getenv('DOCKER_HOST', 'unix:///var/run/docker.sock')
logger.debug(f"Docker host URL: {docker_host}")

client = docker.DockerClient(base_url=docker_host)

try:
    version = client.version()
    logger.debug(f"Docker version: {version}")
except docker.errors.DockerException as e:
    logger.error(f"Docker exception: {e}")

# Load the container name from environment variables
CONTAINER_NAME = os.getenv('DOCKER_CONTAINER_NAME', 'my_container')

# Global variables
FILE_PATH = '/hostmem/gamecontroller-palworld-time_remaining.json'
last_status = None
last_status_update = 0
broadcasted_last = 0
time_lock = threading.Lock()

def load_time_remaining():
    """Load time_remaining from a JSON file, ensuring it's not set to 0."""
    if os.path.exists(FILE_PATH):
        with open(FILE_PATH, 'r') as file:
            data = json.load(file)
            time_remaining = data.get('time_remaining', 900)
            # Check if loaded time_remaining is 0 and set it to 900
            if time_remaining == 0:
                time_remaining = 900
            return time_remaining
    return 900
    
time_remaining = load_time_remaining()

def save_time_remaining(time_remaining):
    """Save time_remaining to a JSON file."""
    with open(FILE_PATH, 'w') as file:
        json.dump({'time_remaining': time_remaining}, file)

@app.route('/stop', methods=['POST'])
def stop_container():
    if request.remote_addr != '127.0.0.1' and request.remote_addr != '::1':
        logger.warning(f"Unauthorized access attempt from IP: {request.remote_addr}")
        abort(403)  # Forbidden
        
    global time_remaining, broadcasted_last
    with time_lock:
        try:
            container = client.containers.get(CONTAINER_NAME)
            if container.status == 'running':
                logger.debug(f"Running backup command in container '{CONTAINER_NAME}'")
                exec_result = container.exec_run('backup')

                if exec_result.exit_code == 0:
                    logger.debug(f"Backup command executed successfully: {exec_result.output.decode()}")
                else:
                    logger.error(f"Backup command failed with exit code {exec_result.exit_code}: {exec_result.output.decode()}")

                container.stop()
                logger.debug(f"Container '{CONTAINER_NAME}' stopped")
        except docker.errors.NotFound as e:
            logger.error(f"Container not found: {e}")
        except Exception as e:
            logger.error(f"Error during backup or stopping container: {e}")
        finally:
            time_remaining = 0
            broadcasted_last = 0
    
    return "OK" 

def get_container_status():
    global last_status, last_status_update

    current_time = time.time()
    if current_time - last_status_update >= 30:
        try:
            container = client.containers.get(CONTAINER_NAME)
            last_status = container.status
            last_status_update = current_time
            logger.debug(f"Container status updated: {last_status}")
        except docker.errors.NotFound as e:
            logger.error(f"Container not found: {e}")
            last_status = "unknown"
    
    return last_status

@app.route('/')
def index():
    global time_remaining
    status = get_container_status()
    docker_container_name = os.getenv('DOCKER_CONTAINER_NAME', 'DefaultContainerName')
    logger.debug(f"Container Name {docker_container_name}, status: {status}, Time remaining: {time_remaining}")
    response = make_response(render_template('index.html', docker_container_name=docker_container_name, status=status, time_remaining=time_remaining))
    response.headers['Cache-Control'] = 'no-store'
    return response

@app.route('/start', methods=['POST'])
def start_container():
    global time_remaining, broadcasted_last

    captcha_answer = request.form.get('captcha_answer')
    logger.debug(f"Received CAPTCHA answer: {captcha_answer}")
    logger.debug(f"Expected CAPTCHA answer: {session.get('captcha_answer')}")
    
    if captcha_answer and int(captcha_answer) == session.get('captcha_answer'):  
  
        with time_lock:
            try:
                container = client.containers.get(CONTAINER_NAME)
                if container.status != 'running':
                    container.start()
                    logger.debug(f"Container '{CONTAINER_NAME}' started")
            except docker.errors.NotFound as e:
                logger.error(f"Container not found: {e}")

            time_remaining = 18000
            broadcasted_last = 0
            save_time_remaining(time_remaining)  # Save to file
            logger.debug(f"Timer started with {time_remaining} seconds")
        return redirect(url_for('index'))
    else:
        logger.warning("Invalid CAPTCHA answer.")
        return redirect(url_for('captcha_error'))

@app.route('/update_timer')
def update_timer():
    if request.remote_addr != '127.0.0.1' and request.remote_addr != '::1':
        logger.warning(f"Unauthorized access attempt from IP: {request.remote_addr}")
        abort(403)  # Forbidden

    global time_remaining, broadcasted_last
    with time_lock:
        if time_remaining > 0:
            time_remaining -= 30
            save_time_remaining(time_remaining)  # Save to file
            logger.debug(f"Time remaining updated: {time_remaining}")
            if time_remaining <= 0:
                logger.debug("Time remaining is 0 stopping containers")
                #stop_container()
                url = 'http://localhost:5000/stop'
                response = requests.post(url)
            elif time_remaining < 45 * 60:
                current_time = int(time.time())
                if current_time - broadcasted_last >= 5 * 60:
                    broadcast_remaining_time()
                    broadcasted_last = current_time
    return "OK"
#    return redirect(url_for('index'))

def broadcast_remaining_time():
    global time_remaining
    minutes_remaining = time_remaining // 60
    try:
        container = client.containers.get(CONTAINER_NAME)
        if container.status == 'running':
            exec_result = container.exec_run(f'rcon-cli "Broadcast {minutes_remaining} minutes remaining"')
            if exec_result.exit_code == 0:
                logger.debug(f"Broadcast command executed successfully: {exec_result.output.decode()}")
            else:
                logger.error(f"Broadcast command failed with exit code {exec_result.exit_code}: {exec_result.output.decode()}")
    except docker.errors.NotFound as e:
        logger.error(f"Container not found: {e}")
    except Exception as e:
        logger.error(f"Error executing broadcast command: {e}")

@app.route('/add_time', methods=['POST'])
def add_time():
    global time_remaining, broadcasted_last
    
    captcha_answer = request.form.get('captcha_answer')
    logger.debug(f"Received CAPTCHA answer: {captcha_answer}")
    logger.debug(f"Expected CAPTCHA answer: {session.get('captcha_answer')}")
    
    if captcha_answer and int(captcha_answer) == session.get('captcha_answer'):
        with time_lock:
            additional_time = 4 * 3600  # 4 hours
            time_remaining += additional_time
            logger.debug(f"Added 4 hours, new time remaining: {time_remaining}")
            broadcasted_last = 0
            save_time_remaining(time_remaining)  # Save to file

            try:
                container = client.containers.get(CONTAINER_NAME)
                if container.status == 'running':
                    exec_result = container.exec_run('rcon-cli "Broadcast Added 4 hours"')
                    if exec_result.exit_code == 0:
                        logger.debug(f"Broadcast command executed successfully: {exec_result.output.decode()}")
                    else:
                        logger.error(f"Broadcast command failed with exit code {exec_result.exit_code}: {exec_result.output.decode()}")
            except docker.errors.NotFound as e:
                logger.error(f"Container not found: {e}")
            except Exception as e:
                logger.error(f"Error executing broadcast command: {e}")
        
        return redirect(url_for('index'))
    else:
        logger.warning("Invalid CAPTCHA answer.")
        return redirect(url_for('captcha_error'))

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

@app.route('/trigger_timer', methods=['POST'])
def trigger_timer():
    """ This route should be triggered periodically """
    return update_timer()

def trigger_timer_task():
    """ Trigger the /trigger_timer endpoint in the main app """
    try:
        response = requests.post('http://localhost:5000/trigger_timer')
        logger.debug(f"Triggered /trigger_timer, response status: {response.status_code}")
    except requests.RequestException as e:
        logger.error(f"Error triggering /trigger_timer: {e}")

# Set up the scheduler
scheduler = BackgroundScheduler()
scheduler.add_job(
    func=trigger_timer_task,
    trigger=IntervalTrigger(seconds=30),
    id='timer_trigger_job',
    name='Trigger /trigger_timer every 30 seconds',
    replace_existing=True
)
scheduler.start()

if __name__ == '__main__':
    logger.debug("Starting Flask application")
    app.run(host='0.0.0.0', port=5000, debug=TRUE)  # Main app runs on port 5000
