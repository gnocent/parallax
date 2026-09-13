package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"parallax/internal/auth"
	"parallax/internal/i18n"
)

//go:embed templates
var fichiersGabarits embed.FS

//go:embed static
var fichiersStatiques embed.FS

// donneesPage est le contexte de la mise en page commune. Contenu porte le
// HTML déjà rendu (et donc déjà échappé) d'un gabarit d'écran — voir
// rendrePage.
type donneesPage struct {
	Titre     string
	Identite  *auth.Identite
	CSRFToken string
	Contenu   template.HTML
	Lang      i18n.Lang // langue affichée — sert au lien de bascule du menu
}

func ouiNon(b bool) string {
	if b {
		return "oui"
	}
	return "non"
}

func videSiNil(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// formaterNombre arrondit à deux décimales et retire les zéros superflus
// (40 -> "40", 362.5 -> "362.5"), pour l'affichage des agrégats du
// constructeur de vues sans bruit de virgule flottante.
func formaterNombre(f float64) string {
	arrondi := math.Round(f*100) / 100
	return strconv.FormatFloat(arrondi, 'f', -1, 64)
}

// formaterOctets affiche une taille de fichier en Mo, ou en Go au-delà de
// 1024 Mo — les sauvegardes de l'écran /parametres/sauvegarde tiennent dans
// ces deux ordres de grandeur, inutile d'aller jusqu'au To.
func formaterOctets(o int64) string {
	mo := float64(o) / (1024 * 1024)
	if mo < 1024 {
		return strconv.FormatFloat(mo, 'f', 1, 64) + " Mo"
	}
	return strconv.FormatFloat(mo/1024, 'f', 2, 64) + " Go"
}

func chargerGabarits() *template.Template {
	fonctions := template.FuncMap{
		"ouiNon":         ouiNon,
		"videSiNil":      videSiNil,
		"joindre":        strings.Join,
		"formaterNombre": formaterNombre,
		"formaterOctets": formaterOctets,
		// "t" (traduction) n'a une valeur utile qu'une fois liée à la langue
		// de la requête — voir gabaritsPour. Go exige malgré tout qu'une
		// fonction référencée dans un gabarit existe dans le FuncMap au
		// moment du parsing ; ce repli en français ne sert donc qu'aux
		// chemins (tests, erreurs très précoces) qui exécuteraient s.gabarits
		// directement sans passer par gabaritsPour.
		"t": func(cle string) string { return i18n.T(i18n.FR, cle) },
	}
	return template.Must(
		template.New("racine").Funcs(fonctions).ParseFS(fichiersGabarits,
			"templates/*.html", "templates/*/*.html"))
}

// gabaritsPour clone l'arbre de gabarits déjà analysé et y lie "t" à la
// langue de cette requête. Cloner est l'idiome documenté de html/template
// pour des fonctions qui varient par exécution : le FuncMap ne peut pas
// porter un paramètre supplémentaire à l'appel, seul un nouveau Funcs() sur
// un clone le peut, sans toucher au gabarit partagé ni aux autres requêtes
// en cours. Le coût (quarante gabarits, une poignée d'utilisateurs
// simultanés) est sans commune mesure avec le calcul de capacité que ces
// mêmes requêtes déclenchent déjà.
func (s *serveur) gabaritsPour(lang i18n.Lang) (*template.Template, error) {
	clone, err := s.gabarits.Clone()
	if err != nil {
		return nil, fmt.Errorf("clonage des gabarits pour la langue %s : %w", lang, err)
	}
	return clone.Funcs(template.FuncMap{
		"t": func(cle string) string { return i18n.T(lang, cle) },
	}), nil
}

// titre traduit une clé i18n "titre.*" dans la langue de la requête, pour
// composer l'argument titre de rendrePage — y compris quand l'écran a besoin
// d'y accoler un nom d'entité non traduisible (ex. le nom d'un cluster).
func (s *serveur) titre(r *http.Request, cle string) string {
	return i18n.T(langueDepuisRequete(r), cle)
}

// rendrePage exécute le gabarit nomContenu, puis l'insère dans la mise en
// page commune (navigation, identité, jeton CSRF). nomContenu doit être un
// nom de gabarit chargé par chargerGabarits, propre à l'écran appelant — par
// convention "<entités>_page", par exemple "projets_page".
func (s *serveur) rendrePage(w http.ResponseWriter, r *http.Request, titre, nomContenu string, donnees any) {
	lang := langueDepuisRequete(r)
	gabarits, err := s.gabaritsPour(lang)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	var corps bytes.Buffer
	if err := gabarits.ExecuteTemplate(&corps, nomContenu, donnees); err != nil {
		s.erreurServeur(w, r, fmt.Errorf("rendu de %s : %w", nomContenu, err))
		return
	}

	page := donneesPage{Titre: titre, Contenu: template.HTML(corps.String()), Lang: lang}
	if identite, ok := identiteDepuis(r.Context()); ok {
		page.Identite = &identite
	}
	if session, ok := sessionDepuis(r.Context()); ok {
		page.CSRFToken = session.CSRFToken
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := gabarits.ExecuteTemplate(w, "mise_en_page", page); err != nil {
		// des octets sont déjà partis : impossible de changer le code de
		// statut, on se contente de journaliser.
		log.Printf("%s %s : rendu de mise_en_page interrompu : %v", r.Method, r.URL.Path, err)
	}
}

// rendreFragment exécute un gabarit isolément, sans mise en page — c'est ce
// que renvoient les réponses aux requêtes htmx qui remplacent une ligne, un
// formulaire ou une portion de page.
func (s *serveur) rendreFragment(w http.ResponseWriter, r *http.Request, nomGabarit string, donnees any) {
	gabarits, err := s.gabaritsPour(langueDepuisRequete(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	pasDeCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := gabarits.ExecuteTemplate(w, nomGabarit, donnees); err != nil {
		s.erreurServeur(w, r, fmt.Errorf("rendu du fragment %s : %w", nomGabarit, err))
	}
}

// erreurServeur journalise l'erreur et répond 500. Si des octets ont déjà été
// écrits (rendu de mise en page entamé), l'entête ne peut plus changer : on
// se contente de journaliser.
func (s *serveur) erreurServeur(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("%s %s : %v", r.Method, r.URL.Path, err)
	http.Error(w, "erreur interne", http.StatusInternalServerError)
}

func statiqueRacine() fs.FS {
	sous, err := fs.Sub(fichiersStatiques, "static")
	if err != nil {
		panic(err) // erreur de build embarquée, ne peut arriver en production
	}
	return sous
}
