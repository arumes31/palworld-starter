// Package captcha generates localized story-based math puzzles. The two
// numbers of each puzzle are not written out in the question text; they are
// referenced by the placeholders {N1} and {N2} and rendered as scratch-off
// images so bots cannot read them from the markup.
package captcha

import (
	"fmt"
	mathrand "math/rand"
	"strings"
)

// Challenge is one generated puzzle. Question contains the {N1} and {N2}
// placeholders where the numbers belong.
type Challenge struct {
	Question string
	Num1     int
	Num2     int
	Answer   int
}

// Segment is a piece of a challenge question: either literal text (Num == 0)
// or a reference to scratch image 1 or 2.
type Segment struct {
	Text string
	Num  int
}

// Segments splits the question into text segments and number placeholders in
// display order, for templates to interleave text with scratch images.
func (c Challenge) Segments() []Segment {
	var segs []Segment
	rest := c.Question
	for rest != "" {
		i1 := strings.Index(rest, "{N1}")
		i2 := strings.Index(rest, "{N2}")
		idx, num := -1, 0
		if i1 >= 0 && (i2 < 0 || i1 < i2) {
			idx, num = i1, 1
		} else if i2 >= 0 {
			idx, num = i2, 2
		}
		if idx < 0 {
			segs = append(segs, Segment{Text: rest})
			break
		}
		if idx > 0 {
			segs = append(segs, Segment{Text: rest[:idx]})
		}
		segs = append(segs, Segment{Num: num})
		rest = rest[idx+len("{N1}"):]
	}
	return segs
}

type theme struct {
	Actor   string
	Item    string
	Setting string
}

var themesDe = []theme{
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

var themesEn = []theme{
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

// NumberToWords spells out n (0-999) in the given language. It is no longer
// used inside questions (numbers are shown as scratch images) but is kept as
// a public utility, e.g. for future audio captchas.
func NumberToWords(n int, lang string) string {
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
		}
		return tens[ten] + "-" + ones[one]
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
		return hundred + " " + NumberToWords(r, lang)
	}
	return hundred + NumberToWords(r, lang)
}

// Generate creates a new challenge in the given language ("de" or "en").
func Generate(lang string) Challenge {
	op := "+"
	if mathrand.Intn(2) == 0 {
		op = "-"
	}
	num1 := mathrand.Intn(100) + 100 // 100 to 199
	num2 := mathrand.Intn(99) + 1    // 1 to 99 (never 0, it must render as an image)

	var answer int
	if op == "+" {
		answer = num1 + num2
	} else {
		answer = num1 - num2
	}

	// The numbers are delivered as scratch-off images, so the question text
	// only carries placeholders.
	num1Words := "{N1}"
	num2Words := "{N2}"

	var t theme
	if lang == "de" {
		t = themesDe[mathrand.Intn(len(themesDe))]
	} else {
		t = themesEn[mathrand.Intn(len(themesEn))]
	}

	actor := t.Actor
	if lang == "en" {
		actor = strings.Title(t.Actor)
	}
	item := t.Item
	setting := t.Setting

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

	return Challenge{Question: question, Num1: num1, Num2: num2, Answer: answer}
}
