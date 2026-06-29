package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	mathrand "math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ==================== CONFIG & LOGGING ====================

const timeFilePath = "/hostmem/gamecontroller-palworld-time_remaining.json"

type TimeFileContent struct {
	TimeRemaining int `json:"time_remaining"`
}

// ==================== GLOBAL STATE ====================

type State struct {
	mu            sync.Mutex
	timeRemaining int
}

var (
	globalState *State
	dockerCli   *client.Client
	sessionKey  []byte
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	mathrand.Seed(time.Now().UnixNano())
	
	// Initialize session encryption key
	sessionKey = make([]byte, 32)
	if _, err := rand.Read(sessionKey); err != nil {
		log.Fatalf("Failed to generate random session key: %v", err)
	}
}

func (s *State) GetTimeRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.timeRemaining
}

func (s *State) UpdateTimeRemaining(mutateFn func(int) int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timeRemaining = mutateFn(s.timeRemaining)
	saveTimeRemaining(s.timeRemaining)
	return s.timeRemaining
}

func (s *State) SetTimeRemaining(val int) {
	s.UpdateTimeRemaining(func(_ int) int {
		return val
	})
}

func initDocker() {
	var err error
	dockerCli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Failed to initialize Docker client: %v", err)
	}
}

// ==================== TIME PERSISTENCE ====================


func loadTimeRemaining() int {
	if _, err := os.Stat(timeFilePath); err == nil {
		file, err := os.Open(timeFilePath)
		if err == nil {
			defer file.Close()
			var content TimeFileContent
			if err := json.NewDecoder(file).Decode(&content); err == nil {
				if content.TimeRemaining < 0 {
					return 0
				}
				return content.TimeRemaining
			}
		}
	}
	// Try to ensure parent directory exists
	dir := filepath.Dir(timeFilePath)
	_ = os.MkdirAll(dir, 0755)
	return 900 // Default 15 minutes grace on first launch
}

func saveTimeRemaining(sec int) {
	content := TimeFileContent{TimeRemaining: sec}
	dir := filepath.Dir(timeFilePath)
	_ = os.MkdirAll(dir, 0755)

	file, err := os.Create(timeFilePath)
	if err != nil {
		log.Printf("Failed to save time file: %v", err)
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(content)
}

// ==================== SESSION MANAGEMENT ====================

type SessionData struct {
	CaptchaAnswer int    `json:"captcha_answer"`
	Language      string `json:"language"`
	CsrfToken     string `json:"csrf_token"`
}

func getSession(r *http.Request) *SessionData {
	cookie, err := r.Cookie("session")
	if err != nil {
		return &SessionData{Language: getPreferredLanguage(r)}
	}

	decoded, err := decryptSession(cookie.Value)
	if err != nil {
		return &SessionData{Language: getPreferredLanguage(r)}
	}

	var data SessionData
	if err := json.Unmarshal(decoded, &data); err != nil {
		return &SessionData{Language: getPreferredLanguage(r)}
	}
	return &data
}

func saveSession(w http.ResponseWriter, data *SessionData) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	encrypted, err := encryptSession(bytes)
	if err != nil {
		log.Printf("Session encryption failed: %v", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    encrypted,
		Path:     "/",
		HttpOnly: true,
	})
}

func encryptSession(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func decryptSession(ciphertextStr string) ([]byte, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:nonceSize]
	actualCiphertext := ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, actualCiphertext, nil)
}

func getPreferredLanguage(r *http.Request) string {
	// Check query param first
	if l := r.URL.Query().Get("lang"); l == "de" || l == "en" {
		return l
	}
	accept := r.Header.Get("Accept-Language")
	if strings.Contains(accept, "de") {
		return "de"
	}
	return "en"
}

// ==================== CAPTCHA SYSTEM ====================

type Theme struct {
	Actor   string
	Item    string
	Setting string
}

var themesDe = []Theme{
	{"Abenteurer", "Schätze", "im dichten Wald"},
	{"Zauberer", "Zauberstäbe", "auf dem magischen Berg"},
	{"Ritter", "Schwerter", "in der alten Burg"},
	{"Entdecker", "Karten", "am Ufer des Meeres"},
	{"Jäger", "Pfeile", "in der Wildnis"},
	{"Alchemist", "Tränke", "im Labor"},
	{"Piratenkapitän", "Goldmünzen", "auf hoher See"},
	{"Drachenreiter", "Schuppen", "in den Wolken"},
	{"Gärtner", "Blumen", "im verzauberten Garten"},
	{"Koch", "Zutaten", "in der Küche"},
}

var themesEn = []Theme{
	{"adventurer", "treasures", "in the dense forest"},
	{"wizard", "wands", "on the magical mountain"},
	{"knight", "swords", "in the ancient castle"},
	{"explorer", "maps", "by the seaside"},
	{"hunter", "arrows", "in the wilderness"},
	{"alchemist", "potions", "in the laboratory"},
	{"pirate captain", "gold coins", "on the high seas"},
	{"dragon rider", "scales", "in the clouds"},
	{"gardener", "flowers", "in the enchanted garden"},
	{"cook", "ingredients", "in the kitchen"},
}

func numberToWords(n int, lang string) string {
	onesDe := []string{"null", "eins", "zwei", "drei", "vier", "fünf", "sechs", "sieben", "acht", "neun", "zehn",
		"elf", "zwölf", "dreizehn", "vierzehn", "fünfzehn", "sechzehn", "siebzehn", "achtzehn", "neunzehn"}
	onesEn := []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
		"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}

	tensDe := []string{"", "", "zwanzig", "dreißig", "vierzig", "fünfzig", "sechzig", "siebzig", "achtzig", "neunzig"}
	tensEn := []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

	var ones, tens []string
	if lang == "de" {
		ones = onesDe
		tens = tensDe
	} else {
		ones = onesEn
		tens = tensEn
	}

	if n < 20 {
		return ones[n]
	}
	if n < 100 {
		ten := n / 10
		one := n % 10
		if one == 0 {
			return tens[ten]
		}
		if lang == "de" {
			return ones[one] + "und" + tens[ten]
		} else {
			return tens[ten] + "-" + ones[one]
		}
	}

	h := n / 100
	r := n % 100

	var hundred string
	if lang == "de" {
		if h == 1 {
			hundred = "einhundert"
		} else {
			hundred = ones[h] + "hundert"
		}
	} else {
		if h == 1 {
			hundred = "one hundred"
		} else {
			hundred = ones[h] + " hundred"
		}
	}

	if r == 0 {
		return hundred
	}

	if lang == "en" {
		return hundred + " " + numberToWords(r, lang)
	}
	return hundred + numberToWords(r, lang)
}

func generateCaptcha(lang string) (string, int) {
	op := "+"
	if mathrand.Intn(2) == 0 {
		op = "-"
	}
	num1 := mathrand.Intn(100) + 100 // 100 to 199
	var num2 int
	if op == "-" {
		num2 = mathrand.Intn(100) // 0 to 99
	} else {
		num2 = mathrand.Intn(99) + 1 // 1 to 99
	}

	var answer int
	if op == "+" {
		answer = num1 + num2
	} else {
		answer = num1 - num2
	}

	num1Words := numberToWords(num1, lang)
	num2Words := numberToWords(num2, lang)

	var theme Theme
	if lang == "de" {
		theme = themesDe[mathrand.Intn(len(themesDe))]
	} else {
		theme = themesEn[mathrand.Intn(len(themesEn))]
	}

	actor := theme.Actor
	if lang == "en" {
		actor = strings.Title(theme.Actor)
	}
	item := theme.Item
	setting := theme.Setting

	var intros []string
	if lang == "de" {
		intros = []string{
			fmt.Sprintf("Stell dir vor, in einem epischen Abenteuer: Der %s %s", actor, setting),
			fmt.Sprintf("In einer fernen Welt: Der %s %s", actor, setting),
			fmt.Sprintf("In einer mystischen Geschichte: Der %s %s", actor, setting),
		}
	} else {
		intros = []string{
			fmt.Sprintf("Imagine in an epic adventure: The %s %s", actor, setting),
			fmt.Sprintf("In a distant world: The %s %s", actor, setting),
			fmt.Sprintf("In a mystical story: The %s %s", actor, setting),
		}
	}
	intro := intros[mathrand.Intn(len(intros))]

	var templates []string
	if op == "+" {
		if lang == "de" {
			templates = []string{
				fmt.Sprintf("%s beginnt mit %s %s. Plötzlich findet er %s weitere %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s hat %s %s bei sich. Dann entdeckt er %s weitere %s in einer Truhe.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s zählt %s %s. Plötzlich erscheinen %s neue %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s trägt %s %s. Am Wegesrand findet er %s zusätzliche %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s beginnt mit %s %s. Ein Händler schenkt ihm %s weitere %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s besitzt %s %s. Dann fällt %s %s vom Himmel.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s sammelt %s %s. In einer Höhle entdeckt er %s weitere %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s hat %s %s. Ein Freund gibt ihm %s zusätzliche %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s startet mit %s %s. Am Ende des Pfads findet er %s neue %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s zählt %s %s. Dann wachsen %s neue %s aus dem Boden.", intro, num1Words, item, num2Words, item),
			}
		} else {
			templates = []string{
				fmt.Sprintf("%s starts with %s %s. Suddenly, he finds %s more %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s has %s %s with him. Then he discovers %s more %s in a chest.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s counts %s %s. Suddenly, %s new %s appear.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s carries %s %s. By the roadside, he finds %s additional %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s begins with %s %s. A merchant gives him %s more %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s owns %s %s. Then %s %s fall from the sky.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s collects %s %s. In a cave, he discovers %s more %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s has %s %s. A friend gives him %s extra %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s starts with %s %s. At the end of the path, he finds %s new %s.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s counts %s %s. Then %s new %s grow from the ground.", intro, num1Words, item, num2Words, item),
			}
		}
	} else {
		if lang == "de" {
			templates = []string{
				fmt.Sprintf("%s beginnt mit %s %s. Doch dann verschwinden %s dieser %s im Nebel.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s hat %s %s. Plötzlich lösen sich %s %s in Rauch auf.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s besitzt %s %s. Ein Dieb stiehlt %s davon.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s zählt %s %s. Dann fallen %s %s in einen Abgrund.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s trägt %s %s. %s davon zerbrechen bei einem Sturm.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s sammelt %s %s. Ein Drache verbrennt %s davon.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s hat %s %s. %s werden von einem Fluch zerstört.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s beginnt mit %s %s. Ein starker Wind trägt %s davon.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s besitzt %s %s. %s versinken im Treibsand.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s zählt %s %s. Dann explodieren %s davon.", intro, num1Words, item, num2Words),
			}
		} else {
			templates = []string{
				fmt.Sprintf("%s starts with %s %s. But then %s of these %s disappear in the mist.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s has %s %s. Suddenly, %s %s dissolve into smoke.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s owns %s %s. A thief steals %s of them.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s counts %s %s. Then %s %s fall into a chasm.", intro, num1Words, item, num2Words, item),
				fmt.Sprintf("%s carries %s %s. %s of them break in a storm.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s collects %s %s. A dragon burns %s of them.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s has %s %s. %s are destroyed by a curse.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s begins with %s %s. A strong wind carries %s away.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s owns %s %s. %s sink into quicksand.", intro, num1Words, item, num2Words),
				fmt.Sprintf("%s counts %s %s. Then %s of them explode.", intro, num1Words, item, num2Words),
			}
		}
	}

	base := templates[mathrand.Intn(len(templates))]
	var question string
	if op == "+" {
		if lang == "de" {
			question = fmt.Sprintf("%s Wie viele %s hat er jetzt?", base, item)
		} else {
			question = fmt.Sprintf("%s How many %s does he have now?", base, item)
		}
	} else {
		if lang == "de" {
			question = fmt.Sprintf("%s Wie viele %s bleiben übrig?", base, item)
		} else {
			question = fmt.Sprintf("%s How many %s are left?", base, item)
		}
	}

	return question, answer
}

// ==================== DOCKER CLIENT WRAPPER ====================

var (
	statusCacheStatus string
	statusCacheTime   time.Time
	statusCacheMu     sync.Mutex
)

func getContainerStatus(containerName string) string {
	if dockerCli == nil {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	inspect, err := dockerCli.ContainerInspect(ctx, containerName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "exited"
		}
		log.Printf("Docker inspect error: %v", err)
		return "unknown"
	}
	return inspect.State.Status
}

func getCachedContainerStatus(containerName string) string {
	statusCacheMu.Lock()
	defer statusCacheMu.Unlock()

	if time.Since(statusCacheTime) < 30*time.Second && statusCacheStatus != "" {
		return statusCacheStatus
	}

	status := getContainerStatus(containerName)
	statusCacheStatus = status
	statusCacheTime = time.Now()
	return status
}

func invalidateStatusCache() {
	statusCacheMu.Lock()
	defer statusCacheMu.Unlock()
	statusCacheStatus = ""
	statusCacheTime = time.Time{}
}

func isServerPaused(containerName string) bool {
	if dockerCli == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	options := types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "40",
	}

	reader, err := dockerCli.ContainerLogs(ctx, containerName, options)
	if err != nil {
		return false
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return false
	}

	lines := strings.Split(string(content), "\n")
	paused := false

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.Contains(line, "[AUTO PAUSE] Paused") {
			paused = true
			break
		}
		if strings.Contains(line, "Wakeup!!!") ||
			strings.Contains(line, "Resumed by") ||
			strings.Contains(line, "Player connected") ||
			strings.Contains(line, "Player disconnected") {
			return false
		}
	}
	return paused
}

func getPlayerCount() int {
	hc := &http.Client{Timeout: 4 * time.Second}
	resp, err := hc.Get("http://localhost:8212/v1/api/players")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0
	}

	var r struct {
		Players []interface{} `json:"players"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return 0
	}
	return len(r.Players)
}

func runExecCommand(containerName string, cmd []string) (int, string, error) {
	if dockerCli == nil {
		return -1, "", fmt.Errorf("docker client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	response, err := dockerCli.ContainerExecCreate(ctx, containerName, config)
	if err != nil {
		return -1, "", err
	}

	resp, err := dockerCli.ContainerExecAttach(ctx, response.ID, types.ExecStartCheck{})
	if err != nil {
		return -1, "", err
	}
	defer resp.Close()

	var out bytes.Buffer
	_, _ = io.Copy(&out, resp.Reader)

	inspect, err := dockerCli.ContainerExecInspect(ctx, response.ID)
	if err != nil {
		return -1, out.String(), err
	}

	return inspect.ExitCode, out.String(), nil
}

func safeBroadcast(containerName string, message string) {
	if getCachedContainerStatus(containerName) != "running" || isServerPaused(containerName) {
		log.Println("Broadcast skipped – server paused or not running")
		return
	}

	exitCode, output, err := runExecCommand(containerName, []string{"rcon-cli", "Broadcast " + message})
	if err != nil {
		log.Printf("RCON broadcast error: %v", err)
		return
	}
	if exitCode != 0 {
		log.Printf("RCON broadcast failed: %s", output)
	}
}

func runBackup(containerName string) {
	if getCachedContainerStatus(containerName) != "running" {
		log.Println("Backup skipped: container not running")
		return
	}
	if isServerPaused(containerName) {
		log.Println("Backup skipped: server is auto-paused (no players)")
		return
	}

	log.Println("Running scheduled backup...")
	exitCode, output, err := runExecCommand(containerName, []string{"backup"})
	if err != nil {
		log.Printf("Backup error: %v", err)
		return
	}
	if exitCode == 0 {
		log.Println("Backup completed successfully")
	} else {
		log.Printf("Backup failed: %s", strings.TrimSpace(output))
	}
}

func startContainer(containerName string) error {
	if dockerCli == nil {
		return fmt.Errorf("docker client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := dockerCli.ContainerStart(ctx, containerName, types.ContainerStartOptions{})
	if err == nil {
		invalidateStatusCache()
	}
	return err
}

func stopContainer(containerName string) error {
	if dockerCli == nil {
		return fmt.Errorf("docker client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := getContainerStatus(containerName)
	if status == "running" {
		// Run backup first (ignore errors)
		_, _, _ = runExecCommand(containerName, []string{"backup"})

		timeout := 10
		stopOpts := container.StopOptions{
			Timeout: &timeout,
		}
		err := dockerCli.ContainerStop(ctx, containerName, stopOpts)
		if err == nil {
			invalidateStatusCache()
		}
		return err
	}
	return nil
}

// ==================== DISCORD API WRAPPER ====================

var (
	discordInviteCache     string
	discordInviteCacheTime time.Time
	discordInviteCacheMu   sync.Mutex
)

func getDiscordInvite() string {
	discordInviteCacheMu.Lock()
	defer discordInviteCacheMu.Unlock()

	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	guildID := os.Getenv("DISCORD_GUILD_ID")
	channelID := os.Getenv("DISCORD_CHANNEL_ID")
	fallbackURL := os.Getenv("DISCORD_FALLBACK_URL")
	if fallbackURL == "" {
		fallbackURL = "https://discord.gg/XXXXXINVITENOTFOUNDXXXXXX"
	}

	if botToken == "" || guildID == "" || channelID == "" {
		return fallbackURL
	}

	if discordInviteCache != "" && time.Since(discordInviteCacheTime) < 1*time.Hour {
		return discordInviteCache
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/invites", channelID)
	payload := map[string]interface{}{
		"max_age":   86400,
		"max_uses":  0,
		"temporary": false,
		"unique":    true,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fallbackURL
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fallbackURL
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("Discord invite API error: %v", err)
		return fallbackURL
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			if code, ok := result["code"].(string); ok && code != "" {
				inviteURL := "https://discord.gg/" + code
				discordInviteCache = inviteURL
				discordInviteCacheTime = time.Now()
				return inviteURL
			}
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Discord invite API status %d: %s", resp.StatusCode, string(body))
	}

	return fallbackURL
}

// ==================== WEB HANDLERS ====================

type PageContext struct {
	Language            string
	DockerContainerName string
	Status              string
	TimeRemaining       int
	DiscordUrl          string
	Question            string
	RetryTarget         string
	CsrfToken           string
}

func renderTemplate(w http.ResponseWriter, r *http.Request, tmplName string, ctx PageContext) {
	if ctx.Language == "" {
		ctx.Language = "en"
	}

	t, err := template.New("").Funcs(template.FuncMap{
		"title": func(s string) string {
			if s == "" {
				return ""
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"tojson": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"range1000": func() []int {
			res := make([]int, 1000)
			for i := 0; i < 1000; i++ {
				res[i] = i
			}
			return res
		},
	}).ParseFiles("templates/base.html", "templates/"+tmplName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}

	err = t.ExecuteTemplate(w, "base.html", ctx)
	if err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

func handleIndex(containerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		sessionData := getSession(r)
		status := getCachedContainerStatus(containerName)
		discordUrl := getDiscordInvite()
		remaining := globalState.GetTimeRemaining()

		containerDisplayName := os.Getenv("DOCKER_CONTAINER_NAME")
		if containerDisplayName == "" {
			containerDisplayName = "Palworld Server"
		}

		renderTemplate(w, r, "index.html", PageContext{
			Language:            sessionData.Language,
			DockerContainerName: containerDisplayName,
			Status:              status,
			TimeRemaining:       remaining,
			DiscordUrl:          discordUrl,
		})
	}
}

func handleCaptchaPage(isStart bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionData := getSession(r)
		
		lang := r.URL.Query().Get("lang")
		if lang == "" {
			lang = sessionData.Language
		}
		if lang != "de" && lang != "en" {
			lang = "en"
		}
		sessionData.Language = lang

		// Generate captcha
		question, answer := generateCaptcha(lang)
		sessionData.CaptchaAnswer = answer

		// Ensure CSRF token exists
		if sessionData.CsrfToken == "" {
			tokenBytes := make([]byte, 16)
			_, _ = rand.Read(tokenBytes)
			sessionData.CsrfToken = hex.EncodeToString(tokenBytes)
		}
		saveSession(w, sessionData)

		discordUrl := getDiscordInvite()
		tmplName := "captcha.html"
		if isStart {
			tmplName = "captcha_start.html"
		}

		renderTemplate(w, r, tmplName, PageContext{
			Language:   lang,
			Question:   question,
			DiscordUrl: discordUrl,
			CsrfToken:  sessionData.CsrfToken,
		})
	}
}

func handleCaptchaError() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionData := getSession(r)
		origin := r.URL.Query().Get("origin")
		if origin == "" {
			origin = "add_time"
		}
		discordUrl := getDiscordInvite()

		renderTemplate(w, r, "captcha_error.html", PageContext{
			Language:    sessionData.Language,
			DiscordUrl:  discordUrl,
			RetryTarget: origin,
		})
	}
}

func handleStart(containerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionData := getSession(r)
		submittedCsrf := r.FormValue("csrf_token")
		if submittedCsrf == "" || submittedCsrf != sessionData.CsrfToken {
			http.Error(w, "Forbidden - CSRF verification failed", http.StatusForbidden)
			return
		}

		ansStr := r.FormValue("captcha_answer")
		ans, err := strconv.Atoi(ansStr)
		if err != nil || ans != sessionData.CaptchaAnswer {
			http.Redirect(w, r, "/captcha_error?origin=start_container", http.StatusSeeOther)
			return
		}

		// Clear captcha answer to prevent reuse
		sessionData.CaptchaAnswer = -9999
		saveSession(w, sessionData)

		// Start container if not running
		status := getContainerStatus(containerName)
		if status != "running" {
			if err := startContainer(containerName); err != nil {
				log.Printf("Failed to start container: %v", err)
			} else {
				globalState.UpdateTimeRemaining(func(current int) int {
					val := current
					if val < 900 {
						val = 900
					}
					return val + 14400
				})
				log.Println("Server started + 4 hours added")
			}
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleAddTime(containerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionData := getSession(r)
		submittedCsrf := r.FormValue("csrf_token")
		if submittedCsrf == "" || submittedCsrf != sessionData.CsrfToken {
			http.Error(w, "Forbidden - CSRF verification failed", http.StatusForbidden)
			return
		}

		ansStr := r.FormValue("captcha_answer")
		ans, err := strconv.Atoi(ansStr)
		if err != nil || ans != sessionData.CaptchaAnswer {
			http.Redirect(w, r, "/captcha_error?origin=add_time", http.StatusSeeOther)
			return
		}

		// Clear captcha answer
		sessionData.CaptchaAnswer = -9999
		saveSession(w, sessionData)

		status := getContainerStatus(containerName)
		if status == "running" {
			remaining := globalState.UpdateTimeRemaining(func(current int) int {
				return current + 43200
			})
			log.Printf("+12 hours added, now %dh", remaining/3600)
		}

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func handleStop(containerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// Restrict to localhost / loopback
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		status := getContainerStatus(containerName)
		if status == "running" {
			if err := stopContainer(containerName); err != nil {
				log.Printf("Stop failed: %v", err)
			} else {
				log.Println("Container stopped due to time expiry or manual stop")
			}
		}
		globalState.UpdateTimeRemaining(func(_ int) int {
			return 0
		})

		w.Write([]byte("OK"))
	}
}

// ==================== BACKGROUND TICKERS ====================

func startTimerTicker(containerName string) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			var expired bool
			globalState.UpdateTimeRemaining(func(current int) int {
				if current > 0 {
					val := current - 30
					if val < 0 {
						val = 0
					}
					if val == 0 {
						expired = true
					}
					return val
				}
				return 0
			})

			if expired {
				status := getCachedContainerStatus(containerName)
				if status == "running" {
					log.Println("TIME EXPIRED → stopping container")
					if err := stopContainer(containerName); err != nil {
						log.Printf("Timer container shutdown failed: %v", err)
					}
				}
			}
		}
	}()
}

func startPlayerExtendTicker(containerName string) {
	ticker := time.NewTicker(300 * time.Second)
	go func() {
		for range ticker.C {
			if isServerPaused(containerName) || getCachedContainerStatus(containerName) != "running" {
				continue
			}
			count := getPlayerCount()
			if count > 0 {
				val := globalState.UpdateTimeRemaining(func(current int) int {
					newVal := current + 300
					if newVal > 172800 { // max 48h
						return 172800
					}
					return newVal
				})
				log.Printf("Players online (%d) → +5 min (now %dh)", count, val/3600)
			}
		}
	}()
}

func startBroadcastLinkTicker(containerName string) {
	ticker := time.NewTicker(30 * time.Minute)
	go func() {
		for range ticker.C {
			safeBroadcast(containerName, "to start this server visit https://pal.wowcraft.pw/")
		}
	}()
}

func startBroadcastUrlTicker(containerName string) {
	ticker := time.NewTicker(68 * time.Minute)
	go func() {
		for range ticker.C {
			safeBroadcast(containerName, "https://pal.wowcraft.pw")
		}
	}()
}

func startAutoBackupTicker(containerName string) {
	ticker := time.NewTicker(15 * time.Minute)
	go func() {
		for range ticker.C {
			runBackup(containerName)
		}
	}()
}

func startDiscordRefreshTicker() {
	ticker := time.NewTicker(30 * time.Minute)
	go func() {
		for range ticker.C {
			_ = getDiscordInvite()
		}
	}()
}

// ==================== MAIN ====================

func main() {
	containerName := os.Getenv("DOCKER_CONTAINER_NAME")
	if containerName == "" {
		containerName = "my_container"
	}

	log.Printf("Initializing Palworld Starter with container: %s", containerName)

	initDocker()
	
	// Load initial state
	initialTime := loadTimeRemaining()
	globalState = &State{
		timeRemaining: initialTime,
	}

	// Warm up cache
	_ = getDiscordInvite()

	// Start tickers
	startTimerTicker(containerName)
	startPlayerExtendTicker(containerName)
	startBroadcastLinkTicker(containerName)
	startBroadcastUrlTicker(containerName)
	startAutoBackupTicker(containerName)
	startDiscordRefreshTicker()

	// Set up router
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex(containerName))
	mux.HandleFunc("/captcha", handleCaptchaPage(false))
	mux.HandleFunc("/captcha_start", handleCaptchaPage(true))
	mux.HandleFunc("/captcha_error", handleCaptchaError())
	mux.HandleFunc("/start", handleStart(containerName))
	mux.HandleFunc("/add_time", handleAddTime(containerName))
	mux.HandleFunc("/stop", handleStop(containerName))

	// Static assets handler
	fileServer := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static", fileServer))

	log.Println("Palworld Free Server Controller started on :5000")
	if err := http.ListenAndServe("0.0.0.0:5000", mux); err != nil {
		log.Fatalf("Server run error: %v", err)
	}
}
