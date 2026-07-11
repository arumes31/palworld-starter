// Package captcha generates localized story-based math puzzles. The two
// numbers of each puzzle are not written out in the question text; they are
// referenced by the placeholders {N1} and {N2} and rendered as scratch-off
// images so bots cannot read them from the markup.
//
// Content is organized into tone packs (classic fantasy and family-friendly
// horror). A pack bundles themes, intros, filler middles and story templates
// so every generated puzzle is tonally consistent.
package captcha

import (
	"fmt"
	mathrand "math/rand"
	"regexp"
	"strings"
)

// Challenge is one generated puzzle. Question contains the {N1} and {N2}
// placeholders where the numbers belong. Answer depends on what the closing
// question asks for: the result, the change (Num2) or the start (Num1).
// Fingerprint identifies the theme/template combination so callers can avoid
// showing the same story twice in a row.
type Challenge struct {
	Question    string
	Num1        int
	Num2        int
	Answer      int
	Fingerprint string
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

// theme holds the story ingredients. Gender ("m"/"f") drives article and
// pronoun selection. ItemDative is the German dative plural ("mit den
// Schwertern"); for English it equals Item since English does not inflect.
type theme struct {
	Actor      string
	Gender     string
	Item       string
	ItemDative string
	Setting    string
}

// storyTmpl is a two-sentence story fragment. The format holds 4 or 5 %s
// slots: intro, {N1}, first item slot, {N2} and - when NamesItem is set -
// the item named again. FirstDative marks the first item slot as German
// dative plural (it follows mit/bei/...).
type storyTmpl struct {
	Format      string
	FirstDative bool
	NamesItem   bool
}

// tonePack bundles all content of one tone for one language.
type tonePack struct {
	Themes  []theme
	Intros  []string // "<prefix>: %s %s" (actor phrase, setting)
	Middles []string // filler sentences; {A} = actor phrase, {S} = setting
	Add     []storyTmpl
	Sub     []storyTmpl
}

// ==================== GERMAN CONTENT ====================

var classicDe = tonePack{
	Themes: []theme{
		{"Abenteurer", "m", "Schätze", "Schätzen", "im dichten Wald"},
		{"Zauberer", "m", "Zauberstäbe", "Zauberstäben", "auf dem magischen Berg"},
		{"Ritter", "m", "Schwerter", "Schwertern", "in der alten Burg"},
		{"Entdecker", "m", "Karten", "Karten", "am Ufer des Meeres"},
		{"Jäger", "m", "Pfeile", "Pfeilen", "in der Wildnis"},
		{"Alchemist", "m", "Tränke", "Tränken", "im Labor"},
		{"Piratenkapitän", "m", "Goldmünzen", "Goldmünzen", "auf hoher See"},
		{"Drachenreiter", "m", "Schuppen", "Schuppen", "in den Wolken"},
		{"Gärtner", "m", "Blumen", "Blumen", "im verzauberten Garten"},
		{"Koch", "m", "Zutaten", "Zutaten", "in der Küche"},
		{"Bäcker", "m", "Brötchen", "Brötchen", "in der warmen Backstube"},
		{"Fischer", "m", "Netze", "Netzen", "am ruhigen Fluss"},
		{"Zwerg", "m", "Edelsteine", "Edelsteinen", "in der tiefen Höhle"},
		{"Müller", "m", "Säcke", "Säcken", "in der alten Mühle"},
		{"Wächter", "m", "Fackeln", "Fackeln", "auf dem hohen Turm"},
		{"Mönch", "m", "Kerzen", "Kerzen", "im stillen Kloster"},
		{"Matrose", "m", "Fässer", "Fässern", "auf dem alten Schiff"},
		{"Förster", "m", "Zapfen", "Zapfen", "auf der grünen Lichtung"},
		{"Räuber", "m", "Beutel", "Beuteln", "in der finsteren Schlucht"},
		{"Bauer", "m", "Äpfel", "Äpfeln", "auf dem weiten Feld"},
		{"Uhrmacher", "m", "Zahnräder", "Zahnrädern", "in der kleinen Werkstatt"},
		{"Glöckner", "m", "Glocken", "Glocken", "im hohen Glockenturm"},
		{"Falkner", "m", "Federn", "Federn", "auf der windigen Klippe"},
		{"Sammler", "m", "Muscheln", "Muscheln", "am sandigen Strand"},
		{"Bergsteiger", "m", "Seile", "Seilen", "auf dem eisigen Gipfel"},
		{"Magierin", "f", "Kristalle", "Kristallen", "im Turm der Weisen"},
		{"Heilerin", "f", "Kräuter", "Kräutern", "am Rande des Dorfes"},
		{"Piratin", "f", "Perlen", "Perlen", "auf der Sturminsel"},
		{"Königin", "f", "Kronjuwelen", "Kronjuwelen", "im hohen Schloss"},
		{"Elfe", "f", "Beeren", "Beeren", "im verzauberten Hain"},
	},
	Intros: []string{
		"Stell dir vor, in einem epischen Abenteuer voller Gefahren, Wunder und ungeahnter Wendungen: %s %s",
		"In einer fernen Welt, weit hinter den Grenzen aller bekannten Landkarten: %s %s",
		"In einer mystischen Geschichte, die von Zauber, Mut und uralten Geheimnissen durchdrungen ist: %s %s",
		"Es war einmal, vor sehr langer Zeit, als die Welt noch jung und voller Magie war: %s %s",
		"Tief im Herzen des alten Königreichs, wo grüne Hügel und silberne Flüsse einander begegnen: %s %s",
		"Die alten, in verstaubtem Leder gebundenen Chroniken berichten aus vergessenen Tagen: %s %s",
		"An einem strahlenden Morgen, als die Sonne golden über den Wipfeln aufging: %s %s",
		"Fernab aller bekannten Pfade, dort wo selbst erfahrene Wanderer nur selten hingelangen: %s %s",
		"Wie die weisen Dorfältesten am flackernden Feuer noch heute gerne erzählen: %s %s",
		"Zu Beginn einer großen und beschwerlichen Reise ins Ungewisse: %s %s",
	},
	Middles: []string{
		"Die Reise ist lang und voller Überraschungen, denn hinter jeder Biegung wartet etwas Unerwartetes.",
		"Am Himmel ziehen dunkle Wolken auf, und ein fernes Grollen kündigt ein herannahendes Gewitter an.",
		"Ein kalter Wind fegt über das Land und lässt die Blätter der alten Bäume unruhig rascheln.",
		"In der Ferne ertönt ein seltsames Geräusch, doch so sehr er auch lauscht, es verrät seinen Ursprung nicht.",
		"Er rastet kurz auf einem umgestürzten Baumstamm und schaut sich dabei aufmerksam nach allen Seiten um.",
		"Die Sonne steht tief über dem Horizont und taucht die ganze Gegend in ein warmes, goldenes Licht.",
		"Ein alter Rabe beobachtet ihn schweigend von einem kahlen Ast aus und legt neugierig den Kopf schief.",
		"Der Weg führt über moosige Steine und knorrige Wurzeln, sodass er bei jedem Schritt genau hinsehen muss.",
		"Dichter Nebel zieht langsam durch die Landschaft und verschluckt nach und nach die vertrauten Umrisse.",
		"Er summt eine alte Melodie vor sich hin, die ihm einst seine Großmutter am Feuer beigebracht hat.",
		"Irgendwo in der Nähe plätschert ein Bach, dessen klares Wasser zwischen glatten Kieseln hindurchspringt.",
		"Die Nacht bricht langsam herein, und die ersten Sterne funkeln zaghaft am dunkler werdenden Himmel.",
		"{A} verweilt einen Augenblick {S} und genießt die seltene Stille.",
		"{A} ist nun schon seit Tagen {S} unterwegs, ohne einer Menschenseele zu begegnen.",
		"{A} denkt an die alten Geschichten, die man sich über solche Reisen erzählt.",
		"{A} prüft sein Gepäck gleich zweimal, denn der Weg ist lang und unberechenbar.",
	},
	Add: []storyTmpl{
		{"%s beginnt seine Reise voller Zuversicht mit %s %s, die sorgsam verstaut sind. Als er kaum den ersten Hügel erreicht, findet er wie durch ein Wunder %s weitere %s am Wegesrand.", true, true},
		{"%s hat %s %s bei sich, sorgsam in einem ledernen Beutel verwahrt. Dann entdeckt er, kaum dass er den Deckel anhebt, %s weitere %s tief unten in einer verstaubten Truhe.", false, true},
		{"%s zählt %s %s ganz genau und immer wieder von vorne, um sich nicht zu verrechnen. Plötzlich, wie aus dem Nichts, erscheinen mitten vor ihm %s neue %s, die im Licht schimmern.", false, true},
		{"%s trägt %s %s behutsam über die steinige Straße, ohne dabei jemals ins Stolpern zu geraten. Am moosbewachsenen Wegesrand findet er kurz darauf %s zusätzliche %s zwischen den Farnen.", false, true},
		{"%s beginnt seinen Handelstag guter Dinge mit %s %s, fein säuberlich aufgereiht. Ein freundlicher Händler aus einem fernen Land schenkt ihm daraufhin %s weitere %s.", true, true},
		{"%s besitzt %s %s, die er über viele Jahre hinweg mit großer Mühe zusammengetragen hat. Dann fallen, begleitet von einem sanften Glitzern, %s %s wie von Zauberhand vom klaren Himmel.", false, true},
		{"%s sammelt %s %s mit unendlicher Geduld, obwohl der Tag bereits zur Neige geht. In einer tiefen, von Tropfsteinen gesäumten Höhle entdeckt er schließlich %s weitere %s.", false, true},
		{"%s hat %s %s, die er wie einen kostbaren Schatz hütet und niemandem zeigt. Ein alter, treuer Freund aus Kindertagen gibt ihm wenig später %s zusätzliche %s.", false, true},
		{"%s startet voller Tatendrang in den jungen Morgen mit %s %s im leichten Gepäck. Am staubigen Ende des langen, gewundenen Pfads findet er %s neue %s.", true, true},
		{"%s zählt %s %s bedächtig und mit ruhiger Hand, während die Vögel über ihm singen. Dann wachsen, ganz langsam und wie im Traum, %s neue %s aus dem feuchten Boden.", false, true},
		{"%s hütet %s %s mit wachsamen Augen, als ginge es um sein eigenes Leben. Bald darauf reicht ihm ein eilig herbeigelaufener Bote atemlos %s weitere %s.", false, true},
		{"%s bewahrt %s %s in einem verborgenen Winkel sorgfältig auf, wo kein Dieb je hinsehen würde. Kurz darauf legt ein kichernder, flinker Kobold heimlich %s weitere %s dazu.", false, true},
		{"%s findet %s %s ganz unverhofft im weichen Gras, kaum dass die Sonne aufgegangen ist. Wenig später gräbt er mit bloßen Händen %s weitere %s aus der lockeren Erde aus.", false, true},
		{"%s verwahrt %s %s in einer eisenbeschlagenen Kiste, deren Schloss nur er allein zu öffnen weiß. Ein grimmiger, aber gutmütiger Zwerg überreicht ihm feierlich %s zusätzliche %s.", false, true},
		{"%s bringt %s %s von einer weiten Wanderung mit, staubig und müde, doch überglücklich. Aus einem gut getarnten Versteck hinter einem Felsen holt er %s weitere %s hervor.", false, true},
		{"%s bewacht %s %s die ganze lange Nacht hindurch, ohne auch nur ein einziges Mal die Augen zu schließen. Plötzlich, im ersten Dämmerlicht, liegen wie durch Zauberei %s weitere %s vor ihm.", false, true},
		{"%s versteckt %s %s unter dichtem Laub am Ufer, gut verborgen vor jedem fremden Blick. Ein munter plätschernder Bach spült ihm kurz darauf %s weitere %s ans steinige Ufer.", false, true},
		{"%s stapelt %s %s zu einem ordentlichen Turm, der im Sonnenlicht beinahe zu leuchten scheint. Kurz darauf tauchen wie aus dem Nichts %s neue %s neben ihm auf.", false, true},
		{"%s bekommt %s %s zum Geburtstag geschenkt, fein verpackt und mit einer bunten Schleife versehen. Später am Abend erhält er zu seiner Überraschung noch %s weitere %s dazu.", false, true},
		{"%s ergattert %s %s nach langem, geduldigem Suchen, das ihn beinahe die ganze Hoffnung gekostet hätte. In einem uralten, halb verfallenen Schrein liegen unberührt %s weitere %s bereit.", false, true},
		{"%s füllt seinen abgewetzten Beutel randvoll mit %s %s, bis kaum noch Platz darin bleibt. Danach steckt er mit einem zufriedenen Lächeln noch %s weitere %s hinein.", true, true},
		{"%s reist frühmorgens im ersten Licht mit %s %s wohlgemut los. Unterwegs, an einer verwitterten Wegkreuzung, findet er %s weitere %s im Gras.", true, true},
		{"%s kehrt nach vielen Wochen endlich mit %s %s heil nach Hause zurück. Am großen, eisenbeschlagenen Tor gibt ihm ein wachsamer Wächter %s weitere %s.", true, true},
		{"%s prahlt lautstark und mit stolzgeschwellter Brust mit %s %s vor allen Leuten. Dann gewinnt er bei einem gewagten Wettstreit %s weitere %s dazu.", true, true},
		{"%s handelt auf dem geschäftigen Marktplatz geschickt mit %s %s um den besten Preis. Ein wohlhabender, gut gelaunter Kunde gibt ihm großzügig %s weitere %s obendrauf.", true, true},
		{"%s rastet erschöpft im weichen Sand bei %s %s am rauschenden Meer. Kurz darauf schwemmt die heranrollende Flut sanft %s weitere %s an den Strand an.", true, true},
		{"%s belädt sein schaukelndes Boot bis an den Rand mit %s %s für die weite Fahrt. Am geschäftigen Kai lädt er kurz vor dem Ablegen %s weitere %s dazu.", true, true},
		{"%s hortet %s %s in einer verborgenen Truhe tief unter der Erde, gut geschützt vor Wind und Wetter. Bei goldenem Sonnenaufgang glitzern %s weitere %s verlockend im feuchten Sand.", false, true},
		{"%s schleppt %s %s mühsam und schwer atmend über den Hof herbei, Schritt für Schritt. Ein fleißiger, junger Gehilfe bringt ihm kurz darauf %s weitere %s.", false, true},
		{"%s birgt %s %s an einem sicheren, nur ihm bekannten Ort, fern von neugierigen Blicken. Später am Tag gesellen sich wie von selbst %s weitere %s dazu.", false, true},
	},
	Sub: []storyTmpl{
		{"%s beginnt seinen langen Weg zuversichtlich mit %s %s im Gepäck. Doch dann verschwinden auf unerklärliche Weise %s dieser kostbaren %s spurlos im dichten Nebel.", true, true},
		{"%s hat %s %s fest in seinem Besitz, sorgsam gehütet und gut bewacht. Plötzlich lösen sich vor seinen ungläubigen Augen %s %s in dichten Rauch auf.", false, true},
		{"%s besitzt %s %s, die er über lange Jahre mit viel Fleiß angehäuft hat. Ein flinker, im Dunkeln lauernder Dieb stiehlt ihm heimlich %s davon.", false, false},
		{"%s zählt %s %s mit zittrigen Fingern nach, kaum dass er seinen Augen traut. Dann fallen mit einem lauten Rumpeln %s %s in einen tiefen, dunklen Abgrund.", false, true},
		{"%s trägt %s %s über einen schmalen, windigen Gebirgspfad, hoch über den Wolken. %s davon zerbrechen bei einem heftigen, unerwarteten Sturm.", false, false},
		{"%s sammelt %s %s mit großer Sorgfalt den ganzen Tag lang, bis die Sonne untergeht. Ein riesiger, feuerspeiender Drache verbrennt mit einem einzigen Atemzug %s davon.", false, false},
		{"%s hat %s %s in seiner Obhut, sorgfältig gezählt und gut versteckt. %s davon werden von einem uralten, bösen Fluch unwiderruflich zerstört.", false, false},
		{"%s beginnt seinen beschwerlichen Aufstieg mit %s %s fest im Griff. Ein plötzlich aufkommender, starker Wind trägt ihm %s davon fort.", true, false},
		{"%s besitzt %s %s, die er wie seinen Augapfel hütet und niemals aus den Augen lässt. %s davon versinken langsam und unaufhaltsam im tückischen Treibsand.", false, false},
		{"%s zählt %s %s ganz in Ruhe ein ums andere Mal durch, um sicherzugehen. Dann explodieren mit einem ohrenbetäubenden Knall unversehens %s davon.", false, false},
		{"%s hütet %s %s Tag und Nacht mit größter Wachsamkeit, ohne je zu ruhen. Ein hinterlistiger, kichernder Kobold entwendet ihm unbemerkt %s davon.", false, false},
		{"%s bewahrt %s %s in einer schweren, verschlossenen Truhe sorgsam auf, tief im Keller verborgen. Bei einem plötzlichen, dreisten Überfall verliert er leider %s davon.", false, false},
		{"%s besitzt %s %s in reicher Fülle, mehr als er je selbst gebrauchen könnte. Er verschenkt aus lauter Herzensgüte %s davon an dankbare Waisenkinder.", false, false},
		{"%s trägt %s %s vorsichtig über eine alte, schwankende Hängebrücke, tief unten rauscht das Wasser. Auf der glitschigen Brücke rutschen %s davon in den reißenden Fluss.", false, false},
		{"%s sammelt %s %s mit Bedacht über viele Tage hinweg, immer auf der Hut. Bei stockfinsterer Nacht vergräbt er heimlich %s davon unter einem Baum.", false, false},
		{"%s hortet %s %s in einer windschiefen Scheune am Rande des Feldes, gut vor Regen geschützt. Ein tosender, alles verschlingender Wirbelsturm reißt %s davon mit sich.", false, false},
		{"%s zählt %s %s am frühen Morgen sorgfältig durch, ehe der große Markttag beginnt. Am belebten, lärmenden Markt verkauft er zu einem guten Preis %s davon.", false, false},
		{"%s verwahrt %s %s in einem geheimen Fach seines Wagens, das kaum jemand kennt. Ein dreister, flinker Gauner schnappt sich im Vorbeigehen %s davon.", false, false},
		{"%s trägt %s %s dicht bei sich, versteckt unter dem weiten Mantel, den er fest umklammert. Im dichten Gedränge des Jahrmarkts gehen %s davon unbemerkt verloren.", false, false},
		{"%s besitzt %s %s in stattlicher Zahl, worauf er insgeheim mächtig stolz ist. Als schweren Tribut an den König gibt er widerwillig %s davon ab.", false, false},
		{"%s beginnt seine Wanderung entlang des Ufers mit %s %s wohlgemut. Ein reißender, hochwasserführender Fluss schwemmt ihm %s davon fort.", true, false},
		{"%s reist über weite, staubige Straßen mit %s %s durch fremde Lande. Unterwegs, an einer unübersichtlichen Kreuzung, verliert er %s davon.", true, false},
		{"%s prahlt vor der ganzen versammelten Menge lautstark mit %s %s. Ein neidischer, ehrgeiziger Rivale gewinnt ihm bei einer Wette %s davon ab.", true, false},
		{"%s kehrt nach langer Abwesenheit endlich mit %s %s zufrieden heim. Unterwegs, an einem armen kleinen Dorf, verschenkt er großzügig %s davon.", true, false},
		{"%s handelt den lieben langen Tag geschickt und beharrlich mit %s %s. Am späten Abend, als die Lichter ausgehen, hat er %s davon verkauft.", true, false},
		{"%s zählt %s %s am Abend noch einmal genau nach, ehe er sich zur Ruhe legt. Über die lange, dunkle Nacht zerfallen auf geheimnisvolle Weise %s davon zu Staub.", false, false},
		{"%s bewacht %s %s die halbe Nacht hindurch am flackernden Lagerfeuer, kämpft aber gegen den Schlaf. Ein listiger, rotpelziger Fuchs schleppt im Schutz der Dunkelheit %s davon fort.", false, false},
		{"%s stapelt %s %s in seiner Kammer zu wackligen, hohen Türmen, die bis zur Decke reichen. Beim hektischen Umzug in ein neues Haus verlegt er versehentlich %s davon.", false, false},
		{"%s sammelt %s %s geduldig am Ufer des weiten Sees, während die Wellen leise plätschern. Ein gewaltiger, dunkler Strudel zieht unerwartet %s davon in die Tiefe.", false, false},
		{"%s hat %s %s in großer Eile zusammengerafft, kaum dass die Gefahr sich näherte. Auf der überstürzten Flucht durch den finsteren Wald lässt er %s davon zurück.", false, false},
	},
}

var horrorDe = tonePack{
	Themes: []theme{
		{"Geisterjäger", "m", "Amulette", "Amuletten", "im verfluchten Schloss"},
		{"Totengräber", "m", "Schädel", "Schädeln", "auf dem alten Friedhof"},
		{"Vampirjäger", "m", "Knoblauchzehen", "Knoblauchzehen", "in der dunklen Krypta"},
		{"Grufthüter", "m", "Laternen", "Laternen", "im nebligen Moor"},
		{"Nachtwächter", "m", "Kürbisse", "Kürbissen", "im Spukwald"},
		{"Hexe", "f", "Besen", "Besen", "im finsteren Moor"},
		{"Seherin", "f", "Kristallkugeln", "Kristallkugeln", "in der Nebelhöhle"},
	},
	Intros: []string{
		"In einer sternenlosen, bitterkalten Nacht, in der kein einziger Lichtstrahl den Himmel durchbrach: %s %s",
		"Als die alte, verrostete Kirchturmuhr dumpf und zwölfmal die Mitternacht schlug: %s %s",
		"Tief im nebelverhangenen Schattenreich, wo das Licht der Sonne niemals hingelangt: %s %s",
		"Bei heulendem, eiskaltem Wind und dem fahlen, gespenstischen Licht des Mondes: %s %s",
		"Wie eine uralte, von Generation zu Generation geflüsterte schaurige Sage berichtet: %s %s",
	},
	Middles: []string{
		"Ein eisiger Hauch streicht ihm über den Nacken, und irgendwo knarrt eine Tür, die niemand geöffnet hat.",
		"In der Ferne heult ein einsamer Wolf, und das Echo hallt lange durch die Dunkelheit.",
		"Die Schatten der Bäume greifen wie dürre Finger nach dem schmalen Pfad.",
		"Eine Uhr schlägt dumpf in der Ferne, obwohl weit und breit kein Turm zu sehen ist.",
		"Fledermäuse flattern lautlos über ihn hinweg und verschwinden in der mondlosen Nacht.",
		"Der Boden knirscht bei jedem Schritt, als würde etwas unter der Erde flüstern.",
		"{A} spürt {S} einen kalten Blick im Rücken, doch beim Umdrehen ist dort nur wabernder Nebel.",
		"{A} hält den Atem an, denn mit einem Mal ist es {S} totenstill.",
	},
	Add: []storyTmpl{
		{"%s beginnt seinen unheimlichen Rundgang mit %s %s im dunklen, verfallenen Spukschloss. Aus einem alten, moosbedeckten Grab erheben sich langsam %s weitere %s.", true, true},
		{"%s zählt %s %s mit klammen Fingern im fahlen, flackernden Mondlicht. Ein bleiches, lautlos schwebendes Gespenst reicht ihm freundlich %s weitere %s.", false, true},
		{"%s trägt %s %s mit angehaltenem Atem durch den dichten, wabernden Nebel. Aus einer offenen, düsteren Gruft schweben geräuschlos %s weitere %s herbei.", false, true},
		{"%s hütet %s %s ängstlich und wachsam im uralten, verfluchten Schloss. Ein freundlicher, sanft schimmernder Geist schenkt ihm daraufhin %s zusätzliche %s.", false, true},
		{"%s beginnt seine gespenstische Suche mit %s %s auf dem stillen, nebligen Friedhof. Aus dem dichten, kalten Nebel tauchen ganz unvermittelt %s weitere %s auf.", true, true},
		{"%s sammelt %s %s mit zitternden Händen im silbrigen Licht des vollen Mondes. Ein wild heulender, grauer Wolf bringt ihm aus dem Dickicht %s weitere %s.", false, true},
		{"%s besitzt %s %s, die er in einer verrosteten Laterne gut verborgen hält. In der düsteren, unheilvollen Geisterstunde erscheinen wie aus dem Nichts %s neue %s.", false, true},
		{"%s hält %s %s fest umklammert in der kalten, zitternden Hand. Ein klapperndes, uraltes Skelett legt mit knochigen Fingern %s weitere %s dazu.", false, true},
		{"%s startet furchtlos mit %s %s in den finsteren, raunenden Spukwald hinein. Aus einem hohlen, knarrenden Baum fallen unversehens %s weitere %s.", true, true},
		{"%s zählt %s %s mit banger Miene an der alten, moosüberwucherten Krypta. Plötzlich schweben aus der undurchdringlichen Finsternis %s weitere %s heran.", false, true},
	},
	Sub: []storyTmpl{
		{"%s beginnt seinen Weg mit %s %s durch die zugigen Hallen des Spukschlosses. Doch dann verschlingt ein dichter, undurchdringlicher Nebel unheimlich langsam %s davon.", true, false},
		{"%s hat %s %s dicht bei sich, verborgen unter dem zerschlissenen Umhang, den er fest hält. Ein hungriges, jammerndes Gespenst stiehlt ihm im Vorbeischweben %s davon.", false, false},
		{"%s zählt %s %s mit stockendem Atem auf dem stillen, nebelverhangenen Friedhof. Plötzlich versinken vor seinen Augen %s davon in einem frisch geöffneten Grab.", false, false},
		{"%s trägt %s %s vorsichtig und mit klopfendem Herzen durch den eiskalten Nebel. Eine bleiche, aus dem Dunkel greifende kalte Hand schnappt sich blitzschnell %s davon.", false, false},
		{"%s besitzt %s %s, die er in einer verstaubten Truhe unter Schloss und Riegel hält. Ein uralter, längst vergessener Fluch lässt über Nacht %s davon spurlos verschwinden.", false, false},
		{"%s beginnt seine schaurige Wache mit %s %s tief unten in der modrigen Gruft. Ein Rudel heulender, hungriger Wölfe trägt ihm im Morgengrauen %s davon fort.", true, false},
		{"%s hält %s %s fest und schützend in der zitternden Hand. Ein klapperndes, aus dem Boden steigendes Skelett zerbricht mit dürren Fingern %s davon.", false, false},
		{"%s sammelt %s %s mit angehaltenem Atem im gespenstischen Licht des Vollmonds. In der unheimlichen Geisterstunde lösen sich lautlos %s davon in dünne Luft auf.", false, false},
		{"%s startet zögernd mit %s %s in den tief verschneiten, unheimlichen Spukwald. Ein riesiger, lautlos gleitender dunkler Schatten verschluckt gierig %s davon.", true, false},
		{"%s zählt %s %s mit bebender Stimme an der verwitterten, halb versunkenen Krypta. Dann fallen mit einem dumpfen Hall %s davon in einen tiefen, schwarzen Brunnen.", false, false},
	},
}

// ==================== ENGLISH CONTENT ====================

var classicEn = tonePack{
	Themes: []theme{
		{"adventurer", "m", "treasures", "treasures", "in the dense forest"},
		{"wizard", "m", "wands", "wands", "on the magical mountain"},
		{"knight", "m", "swords", "swords", "in the ancient castle"},
		{"explorer", "m", "maps", "maps", "by the seaside"},
		{"hunter", "m", "arrows", "arrows", "in the wilderness"},
		{"alchemist", "m", "potions", "potions", "in the laboratory"},
		{"pirate captain", "m", "gold coins", "gold coins", "on the high seas"},
		{"dragon rider", "m", "scales", "scales", "in the clouds"},
		{"gardener", "m", "flowers", "flowers", "in the enchanted garden"},
		{"cook", "m", "ingredients", "ingredients", "in the kitchen"},
		{"baker", "m", "loaves", "loaves", "in the warm bakery"},
		{"fisherman", "m", "nets", "nets", "by the calm river"},
		{"dwarf", "m", "gemstones", "gemstones", "in the deep cave"},
		{"miller", "m", "sacks", "sacks", "in the old mill"},
		{"watchman", "m", "torches", "torches", "on the high tower"},
		{"monk", "m", "candles", "candles", "in the quiet monastery"},
		{"sailor", "m", "barrels", "barrels", "on the old ship"},
		{"forester", "m", "pinecones", "pinecones", "in the green clearing"},
		{"bandit", "m", "pouches", "pouches", "in the dark ravine"},
		{"farmer", "m", "apples", "apples", "in the wide field"},
		{"clockmaker", "m", "gears", "gears", "in the small workshop"},
		{"bell ringer", "m", "bells", "bells", "in the tall belfry"},
		{"falconer", "m", "feathers", "feathers", "on the windy cliff"},
		{"beachcomber", "m", "seashells", "seashells", "on the sandy shore"},
		{"climber", "m", "ropes", "ropes", "on the icy peak"},
		{"sorceress", "f", "crystals", "crystals", "in the tower of the wise"},
		{"healer", "f", "herbs", "herbs", "at the edge of the village"},
		{"pirate queen", "f", "pearls", "pearls", "on the storm island"},
		{"queen", "f", "crown jewels", "crown jewels", "in the high castle"},
		{"elf maiden", "f", "berries", "berries", "in the enchanted grove"},
	},
	Intros: []string{
		"Imagine, in an epic adventure told and retold across countless firelit evenings: %s %s",
		"In a distant world, far beyond the edges of every map ever drawn: %s %s",
		"In a mystical story whispered from one generation to the next: %s %s",
		"Once upon a time, long ago, before the oldest of the old oaks had taken root: %s %s",
		"Deep in the heart of the kingdom, where banners snap in the wind above golden fields: %s %s",
		"As the old chronicles tell, in ink long since faded upon their brittle pages: %s %s",
		"On a bright and cloudless morning, with dew still glittering upon every blade of grass: %s %s",
		"Far from all known paths, past the last signpost any traveler has ever bothered to raise: %s %s",
		"As the village elders recall, nodding slowly beside the flickering warmth of the hearth: %s %s",
		"At the start of a great journey, with the whole wide horizon waiting patiently ahead: %s %s",
	},
	Middles: []string{
		"The journey is long and full of surprises, for something unexpected waits around every single bend.",
		"Dark clouds gather on the horizon, and a distant rumble announces a storm drawing steadily closer.",
		"A cold wind sweeps across the land and sets the leaves of the ancient trees rustling restlessly.",
		"A strange sound echoes in the distance, yet no matter how hard he listens, its source stays hidden.",
		"He rests briefly on a fallen tree trunk and looks around carefully in every direction as he does.",
		"The sun hangs low over the horizon and bathes the whole countryside in a warm, golden light.",
		"An old raven watches him in silence from a bare branch, tilting its head with quiet curiosity.",
		"The path winds over mossy stones and gnarled roots, so he has to watch his footing with every step.",
		"Thick fog drifts slowly through the landscape, swallowing the familiar shapes one after another.",
		"He hums an old tune to himself, one his grandmother taught him by the fireside long ago.",
		"Somewhere nearby a stream murmurs, its clear water dancing between smooth and shining pebbles.",
		"Night is slowly falling, and the first stars twinkle faintly in the darkening sky above him.",
		"{A} pauses for a while {S} and takes in the rare moment of calm.",
		"{A} has been wandering {S} for days now without meeting a single soul.",
		"{A} recalls the old tales that the elders once told about journeys like this one.",
		"{A} checks his gear twice, for the road ahead is long and unpredictable.",
	},
	Add: []storyTmpl{
		{"%s sets out at dawn with %s %s stowed safely in his worn leather pack. Suddenly, as the trail bends through the trees, he finds %s more %s lying in plain sight.", false, true},
		{"%s has %s %s with him as he wanders the long road. Then, prying open a dusty lid, he discovers %s more %s tucked inside an old chest.", false, true},
		{"%s counts %s %s slowly, one by one, beneath the wide morning sky. Suddenly, as if summoned by magic, %s new %s appear before his very eyes.", false, true},
		{"%s carries %s %s along the dusty, sunlit road. By the mossy roadside, half hidden among the ferns, he finds %s additional %s.", false, true},
		{"%s begins his morning with %s %s already in hand. A kindly merchant, smiling warmly beneath a striped awning, gives him %s more %s.", false, true},
		{"%s owns %s %s and guards them well. Then, in a shower of glittering wonder, %s %s fall gently from the sky.", false, true},
		{"%s collects %s %s with patient, practiced care. In a cool and shadowed cave, hidden far from the light, he discovers %s more %s.", false, true},
		{"%s has %s %s that he prizes dearly. A loyal friend, generous as ever, gives him %s extra %s.", false, true},
		{"%s starts with %s %s and a heart full of hope. At the very end of the winding path, where few travelers dare to tread, he finds %s new %s.", false, true},
		{"%s counts %s %s in the pale light of dawn. Then, as the soil stirs and shifts, %s new %s grow slowly from the ground.", false, true},
		{"%s gathers %s %s at dawn while the mist still clings to the hills. A kind traveler, weary from the long road, hands him %s more %s.", false, true},
		{"%s keeps %s %s in his pack, wrapped in soft cloth for safety. Soon, while resting beneath a broad oak, he stumbles upon %s more %s hidden under a rock.", false, true},
		{"%s guards %s %s with unwavering vigilance day and night. A grateful villager, thankful for his kindness, rewards him with %s more %s.", false, true},
		{"%s holds %s %s close within his careful grasp. A sudden and unexpected stroke of luck, bright as a falling star, brings him %s additional %s.", false, true},
		{"%s stores %s %s in his satchel for the road ahead. Then, cheered on by a merry crowd, he wins %s more %s in a friendly contest.", false, true},
		{"%s tallies %s %s with a quiet nod of satisfaction. An old friend, arriving without a word of warning, surprises him with %s extra %s.", false, true},
		{"%s displays %s %s proudly for all the market to admire. A wandering merchant, laden with curious goods, trades him %s more %s.", false, true},
		{"%s hides %s %s in a sturdy chest buried deep in the earth. Later, digging by candlelight, he unearths %s more %s beneath the floor.", false, true},
		{"%s carries %s %s across the old wooden bridge that sways in the breeze. On the far side, where the path grows green again, he collects %s more %s.", false, true},
		{"%s counts %s %s by the warm and crackling fire. A generous stranger, passing quietly through the night, adds %s more %s to the pile.", false, true},
		{"%s packs %s %s for the long journey that lies ahead. Along the dusty trail, beneath the noonday sun, he gathers %s additional %s.", false, true},
		{"%s already owns %s %s earned through many hard days. A hidden shrine, veiled in ivy and silence, grants him %s more %s.", false, true},
		{"%s begins the day with %s %s and a spring in his step. A passing caravan, heavy with dust and distant trade, offers him %s more %s.", false, true},
		{"%s cherishes %s %s beyond all silver and gold. A loyal companion, ever faithful at his side, brings him %s extra %s.", false, true},
		{"%s arranges %s %s neatly in a tidy, gleaming row. Then, to his great delight and surprise, he receives %s more %s as a heartfelt gift.", false, true},
		{"%s clutches %s %s tightly against the biting cold. A wise elder, robed in grey and leaning on a staff, bestows %s more %s upon him.", false, true},
		{"%s piles up %s %s into a towering, careful heap. A sudden turn of fortune, as welcome as spring rain, yields him %s additional %s.", false, true},
		{"%s totals %s %s beneath the quiet stars above. During the deep and silent night, while the camp lies sleeping, he finds %s more %s glinting nearby.", false, true},
		{"%s assembles %s %s with steady and patient hands. A friendly spirit, kind despite a ghostly pallor, leaves him %s more %s at his door.", false, true},
		{"%s stacks %s %s in a neat and orderly mound. Then a helpful gnome, whistling a cheerful little tune, delivers %s more %s to his camp.", false, true},
	},
	Sub: []storyTmpl{
		{"%s sets out with %s %s clutched close to his chest. But then, as a cold grey fog rolls in, %s of these %s disappear into the mist.", false, true},
		{"%s has %s %s gathered over many long days. Suddenly, with a hiss and a curl of vapor, %s %s dissolve into thin smoke.", false, true},
		{"%s owns %s %s and keeps them ever near. A quick and cunning thief, slipping through the crowd, steals %s of them.", false, false},
		{"%s counts %s %s in the gathering dusk. Then, without a single sound of warning, %s %s fall into a yawning chasm.", false, true},
		{"%s carries %s %s across the storm-lashed moor. Battered by wind and rain, %s of them break in the fury of the storm.", false, false},
		{"%s collects %s %s with pride and care. A great red dragon, breathing fire from above, burns %s of them.", false, false},
		{"%s has %s %s from a long and weary quest. Struck by dark magic none can undo, %s of them are destroyed by a bitter curse.", false, false},
		{"%s begins with %s %s upon the open plain. A strong and howling wind, sweeping across the grass, carries %s of them away.", false, false},
		{"%s owns %s %s and treads with care. Where the ground turns soft and treacherous, %s of them sink into the quicksand.", false, false},
		{"%s counts %s %s beside the smoldering coals. Then, with a sharp and startling crack, %s of them explode in a burst of sparks.", false, false},
		{"%s guards %s %s through the long dark hours. A sly and silent goblin, creeping in unseen, snatches %s of them overnight.", false, false},
		{"%s holds %s %s in his weary hands. Sadly, through a narrow crack in the old floor, %s of them slip away.", false, false},
		{"%s stores %s %s in his heavy pack. A swift and swollen river, roaring past the bank, washes %s of them downstream.", false, false},
		{"%s carries %s %s along the moonlit road. A grinning trickster, quick of hand and eye, makes %s of them vanish in a puff.", false, false},
		{"%s counts %s %s at the mountain edge, high and dizzy. Then, slipping loose from his grasp, %s of them tumble off the cliff.", false, false},
		{"%s owns %s %s of dazzling worth. A greedy raven, black wings beating hard, flies off with %s of them.", false, false},
		{"%s keeps %s %s safe as best he can. During the raging midnight storm, %s of them are swept away.", false, false},
		{"%s packs %s %s for the road ahead. A hungry and lumbering troll, drooling in the dark, gobbles up %s of them.", false, false},
		{"%s gathers %s %s with quiet joy. A mischievous little imp, giggling all the while, hides %s of them.", false, false},
		{"%s tallies %s %s by the fading light. Then, worn thin by the passing years, %s of them crumble into fine grey dust.", false, false},
		{"%s clutches %s %s against his beating heart. A sudden and swelling flood, rising without warning, carries %s of them off.", false, false},
		{"%s stacks %s %s in a careful little tower. A sharp and sudden gust of wind, shrieking through the pass, scatters %s of them beyond reach.", false, false},
		{"%s piles up %s %s in the flickering gloom. A tall and shadowy figure, cloaked from head to toe, spirits away %s of them.", false, false},
		{"%s hides %s %s in a cool dark cave. With a thunderous roar, a sudden rockslide buries %s of them forever.", false, false},
		{"%s cherishes %s %s more than words can say. Alas, lost far down where no light reaches, %s of them vanish in a deep ravine.", false, false},
		{"%s displays %s %s for the whole town to see. A cunning russet fox, quick as a shadow, darts off with %s of them.", false, false},
		{"%s arranges %s %s in a gleaming, tidy row. Then, one by one in the flickering dark, %s of these %s slip into a bottomless well.", false, true},
		{"%s holds %s %s in his trembling hands. A roaring, hungry flame, leaping high into the night, consumes %s of these %s.", false, true},
		{"%s counts %s %s at the restless water edge. A churning, greedy whirlpool, spinning ever faster, drags %s of these %s beneath the waves.", false, true},
		{"%s carries %s %s across the trembling ground. Suddenly, as the earth splits wide with a groan, %s of these %s are swallowed whole.", false, true},
	},
}

var horrorEn = tonePack{
	Themes: []theme{
		{"ghost hunter", "m", "amulets", "amulets", "in the cursed castle"},
		{"gravedigger", "m", "skulls", "skulls", "in the old graveyard"},
		{"vampire hunter", "m", "garlic cloves", "garlic cloves", "in the dark crypt"},
		{"crypt keeper", "m", "lanterns", "lanterns", "in the foggy moor"},
		{"night watchman", "m", "pumpkins", "pumpkins", "in the haunted forest"},
		{"witch", "f", "broomsticks", "broomsticks", "in the gloomy moor"},
		{"seer", "f", "crystal balls", "crystal balls", "in the misty cave"},
	},
	Intros: []string{
		"On a starless night, when even the boldest owls fell silent in the trees: %s %s",
		"As the church bell struck midnight, its echo trembling through the empty streets: %s %s",
		"Deep in the realm of shadows, where the cold mist never fully lifts: %s %s",
		"Under a pale and misty moon, half hidden behind ragged drifting clouds: %s %s",
		"As a chilling tale recounts, told in hushed whispers around a dying fire: %s %s",
	},
	Middles: []string{
		"An icy breath brushes the back of his neck, and somewhere a door creaks that no one has opened.",
		"A lone wolf howls in the distance, and the echo rolls on through the darkness for a long time.",
		"The shadows of the trees reach for the narrow path like withered fingers.",
		"A clock tolls dully in the distance, though no tower can be seen for miles.",
		"Bats flutter silently overhead and vanish into the moonless night.",
		"The ground crunches with every step, as if something beneath the earth were whispering.",
		"{A} feels a cold gaze upon his back {S}, but when he turns there is only drifting fog.",
		"{A} holds his breath, for {S} everything has suddenly fallen deathly silent.",
	},
	Add: []storyTmpl{
		{"%s starts with %s %s deep inside the haunted castle. Suddenly, drifting down the cold stone stair, a friendly ghost hands him %s more %s.", false, true},
		{"%s counts %s %s by the thin and pale moonlight. From an old and crumbling grave, wreathed in creeping mist, rise %s more %s.", false, true},
		{"%s carries %s %s through the swirling grey fog. With a hollow clatter of ancient bones, a rattling skeleton adds %s more %s.", false, true},
		{"%s guards %s %s in the silent graveyard. Out of the low and clinging mist, pale as milk, drift %s more %s.", false, true},
		{"%s begins with %s %s in the spooky moonlit forest. From somewhere in the dark, a howling wolf brings him %s more %s.", false, true},
		{"%s holds %s %s in his cold and trembling hand. During the eerie hush of the witching hour, %s more %s quietly appear.", false, true},
		{"%s owns %s %s and keeps them close. From a gnarled and hollow tree, deep in the wood, fall %s more %s.", false, true},
		{"%s gathers %s %s beneath the swollen full moon. A pale and flickering spirit, drifting through the dark, gives him %s more %s.", false, true},
		{"%s starts with %s %s near the old crumbling crypt. Out of the deep and gathering shadows, soft as breath, float %s more %s.", false, true},
		{"%s counts %s %s in the thick and eerie fog. Then, rising slowly from the cold damp ground, %s more %s take shape.", false, true},
	},
	Sub: []storyTmpl{
		{"%s starts with %s %s within the haunted castle walls. But then, creeping in through every crack, the thick grey fog swallows %s of them.", false, false},
		{"%s has %s %s with him on the lonely road. A hungry and mournful ghost, wailing in the dark, steals %s of them.", false, false},
		{"%s counts %s %s in the silent graveyard. Suddenly, as the cold earth yawns apart, %s of them sink into an open grave.", false, false},
		{"%s carries %s %s through the swirling fog. Out of the murk, quick and pale, a cold hand snatches %s of them.", false, false},
		{"%s owns %s %s and guards them close. Woven long ago by wicked words, an ancient curse makes %s of them vanish.", false, false},
		{"%s begins with %s %s deep in the crypt. Baying beneath the pale and swollen moon, howling wolves carry %s of them away.", false, false},
		{"%s holds %s %s in his shaking hand. With a clatter of dry and ancient bone, a rattling skeleton shatters %s of them.", false, false},
		{"%s gathers %s %s under the swollen full moon. During the eerie hush of the witching hour, %s of them dissolve into thin air.", false, false},
		{"%s starts with %s %s in the spooky moonlit forest. Slithering silently across the ground, a creeping shadow devours %s of them.", false, false},
		{"%s counts %s %s near the crumbling crypt. Then, tumbling down into the cold black dark, %s of them fall into a deep well.", false, false},
	},
}

var packsDe = []tonePack{classicDe, horrorDe}
var packsEn = []tonePack{classicEn, horrorEn}

// ==================== QUESTIONS ====================

// Closing question variants; %s is the item in base plural form. Which pool
// is used decides what the answer is: the result, the change (num2) or the
// starting amount (num1).
var questionsResultAddDe = []string{
	"Wie viele %s hat er jetzt?",
	"Wie viele %s nennt er nun sein Eigen?",
	"Wie viele %s besitzt er am Ende?",
	"Wie viele %s trägt er jetzt bei sich?",
	"Kannst du sagen, wie viele %s er nun hat?",
	"Weißt du, wie viele %s es nun insgesamt sind?",
}

var questionsResultSubDe = []string{
	"Wie viele %s bleiben übrig?",
	"Wie viele %s sind ihm noch geblieben?",
	"Wie viele %s hat er danach noch?",
	"Wie viele %s hält er am Ende noch in den Händen?",
	"Kannst du sagen, wie viele %s ihm bleiben?",
	"Weißt du, wie viele %s übrig sind?",
}

var questionsDeltaAddDe = []string{
	"Wie viele %s kamen neu hinzu?",
	"Wie viele %s hat er neu dazubekommen?",
	"Um wie viele %s ist sein Vorrat gewachsen?",
}

var questionsDeltaSubDe = []string{
	"Um wie viele %s hat sich sein Vorrat verringert?",
	"Wie viele %s hat er auf einmal weniger?",
	"Wie viele %s sind nicht mehr bei ihm?",
}

var questionsStartDe = []string{
	"Wie viele %s hatte er ganz zu Beginn?",
	"Wie viele %s waren es ganz am Anfang?",
	"Wie viele %s besaß er zu Beginn der Geschichte?",
}

var questionsResultAddEn = []string{
	"How many %s does he have now?",
	"How many %s does he now call his own?",
	"How many %s does he own in the end?",
	"How many %s does he carry with him now?",
	"Can you tell how many %s he has now?",
	"Do you know how many %s there are now in total?",
}

var questionsResultSubEn = []string{
	"How many %s are left?",
	"How many %s remain in his keeping?",
	"How many %s does he still have afterwards?",
	"How many %s does he hold in his hands at the end?",
	"Can you tell how many %s remain?",
	"Do you know how many %s are left?",
}

var questionsDeltaAddEn = []string{
	"How many %s were newly added?",
	"How many %s did he newly receive?",
	"By how many %s did his stock grow?",
}

var questionsDeltaSubEn = []string{
	"By how many %s did his stock shrink?",
	"How many %s fewer does he have now?",
	"How many %s are no longer with him?",
}

var questionsStartEn = []string{
	"How many %s did he have at the very beginning?",
	"How many %s were there at the start?",
	"How many %s did he own before it all happened?",
}

// ==================== GENERATION ====================

// insertMiddles splices 1-2 filler sentences between the first and second
// sentence of the story, so the two numbers never appear back to back. {A}
// and {S} in a middle are replaced with the actor phrase and setting. At
// most one actor-naming middle is used per story - the first sentence
// already names the actor, and repeating the name in every sentence reads
// clumsily.
func insertMiddles(story string, middles []string, actorPhrase, setting string) string {
	idx := strings.Index(story, ". ")
	if idx < 0 {
		return story
	}

	count := 1 + mathrand.Intn(2)
	picked := make([]string, 0, count)
	first := mathrand.Intn(len(middles))
	usedActor := false
	for i := 0; len(picked) < count && i < len(middles); i++ {
		m := middles[(first+i)%len(middles)]
		if strings.Contains(m, "{A}") {
			if usedActor {
				continue
			}
			usedActor = true
			m = strings.ReplaceAll(m, "{A}", actorPhrase)
		}
		m = strings.ReplaceAll(m, "{S}", setting)
		picked = append(picked, m)
	}

	return story[:idx+2] + strings.Join(picked, " ") + " " + story[idx+2:]
}

var (
	pronounsDe = regexp.MustCompile(`\b(Er|er|Ihm|ihm|Ihn|ihn|Seinen|seinen|Seinem|seinem|Seiner|seiner|Seine|seine|Sein|sein)\b`)
	feminineDe = map[string]string{
		"Er": "Sie", "er": "sie", "Ihm": "Ihr", "ihm": "ihr", "Ihn": "Sie", "ihn": "sie",
		"Seinen": "Ihren", "seinen": "ihren", "Seinem": "Ihrem", "seinem": "ihrem",
		"Seiner": "Ihrer", "seiner": "ihrer", "Seine": "Ihre", "seine": "ihre",
		"Sein": "Ihr", "sein": "ihr",
	}

	pronounsEn = regexp.MustCompile(`\b(He|he|Him|him|His|his)\b`)
	feminineEn = map[string]string{
		"He": "She", "he": "she", "Him": "Her", "him": "her", "His": "Her", "his": "her",
	}
)

// feminize rewrites the masculine pronouns of a story (all of which refer to
// the actor) into their feminine forms.
func feminize(text, lang string) string {
	re, table := pronounsEn, feminineEn
	if lang == "de" {
		re, table = pronounsDe, feminineDe
	}
	return re.ReplaceAllStringFunc(text, func(w string) string {
		if f, ok := table[w]; ok {
			return f
		}
		return w
	})
}

func actorPhrase(t theme, lang string) string {
	if lang == "de" {
		article := "Der"
		if t.Gender == "f" {
			article = "Die"
		}
		return article + " " + t.Actor
	}
	return "The " + strings.Title(t.Actor)
}

// Generate creates a new challenge in the given language ("de" or "en").
func Generate(lang string) Challenge {
	// Pick tone pack: mostly classic fantasy, sometimes spooky.
	packs := packsEn
	if lang == "de" {
		packs = packsDe
	}
	packIdx := 0
	if mathrand.Intn(4) == 0 {
		packIdx = 1
	}
	pack := packs[packIdx]

	themeIdx := mathrand.Intn(len(pack.Themes))
	t := pack.Themes[themeIdx]
	actor := actorPhrase(t, lang)
	intro := fmt.Sprintf(pack.Intros[mathrand.Intn(len(pack.Intros))], actor, t.Setting)

	op := "+"
	if mathrand.Intn(2) == 0 {
		op = "-"
	}
	num1 := mathrand.Intn(100) + 100 // 100 to 199
	num2 := mathrand.Intn(98) + 2    // 2 to 99: never 0 (must render as an image), never 1 (all stories are worded in plural)

	// The numbers are delivered as scratch-off images, so the question text
	// only carries placeholders.
	tmpls := pack.Add
	if op == "-" {
		tmpls = pack.Sub
	}
	tmplIdx := mathrand.Intn(len(tmpls))
	st := tmpls[tmplIdx]

	firstItem := t.Item
	if st.FirstDative {
		firstItem = t.ItemDative
	}
	var base string
	if st.NamesItem {
		base = fmt.Sprintf(st.Format, intro, "{N1}", firstItem, "{N2}", t.Item)
	} else {
		base = fmt.Sprintf(st.Format, intro, "{N1}", firstItem, "{N2}")
	}
	base = insertMiddles(base, pack.Middles, actor, t.Setting)

	// Vary what is asked: the result (default), the change, or the start.
	// This keeps the required arithmetic unpredictable for bots.
	var answer int
	var questionForms []string
	switch mathrand.Intn(4) {
	case 0: // change (num2)
		answer = num2
		switch {
		case op == "+" && lang == "de":
			questionForms = questionsDeltaAddDe
		case op == "+":
			questionForms = questionsDeltaAddEn
		case lang == "de":
			questionForms = questionsDeltaSubDe
		default:
			questionForms = questionsDeltaSubEn
		}
	case 1: // starting amount (num1)
		answer = num1
		questionForms = questionsStartEn
		if lang == "de" {
			questionForms = questionsStartDe
		}
	default: // result of the operation
		if op == "+" {
			answer = num1 + num2
		} else {
			answer = num1 - num2
		}
		switch {
		case op == "+" && lang == "de":
			questionForms = questionsResultAddDe
		case op == "+":
			questionForms = questionsResultAddEn
		case lang == "de":
			questionForms = questionsResultSubDe
		default:
			questionForms = questionsResultSubEn
		}
	}
	form := questionForms[mathrand.Intn(len(questionForms))]
	question := base + " " + fmt.Sprintf(form, t.Item)

	if t.Gender == "f" {
		question = feminize(question, lang)
	}

	return Challenge{
		Question:    question,
		Num1:        num1,
		Num2:        num2,
		Answer:      answer,
		Fingerprint: fmt.Sprintf("%s|%d|%d|%s|%d", lang, packIdx, themeIdx, op, tmplIdx),
	}
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
			// "eins" loses its -s inside compounds: 21 = "einundzwanzig".
			oneWord := ones[one]
			if one == 1 {
				oneWord = "ein"
			}
			return oneWord + "und" + tens[ten]
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
