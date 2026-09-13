// Package i18n porte le catalogue de traduction de l'interface de Parallax
// (français par défaut, anglais en option — voir docs/guide-utilisateur.md).
//
// Ce catalogue couvre l'habillage de l'application : navigation, libellés,
// boutons, titres d'écran. Il ne couvre volontairement pas les messages
// d'erreur métier, construits dynamiquement dans internal/depot et
// internal/web (voir CONTRIBUTING.md) : les traduire demanderait de
// transformer chaque erreur en un couple code + paramètres, un chantier
// distinct et plus profond que l'habillage visuel.
//
// Organisation en plusieurs fichiers catalogue_*.go, chacun n'apportant que
// ses propres clés via Ajouter (appelée depuis un init()) : un écran ou un
// groupe d'écrans se traduit dans son propre fichier, sans jamais modifier
// celui d'un autre — important quand plusieurs personnes (ou plusieurs
// passes) traduisent des écrans différents en parallèle. Ajouter panique
// sur une clé déjà posée par un autre fichier : un doublon de nom se
// découvre au démarrage du programme (donc dès `go test`), jamais en
// production.
//
// Convention pour ajouter une clé : l'ajouter aux DEUX langues, dans le
// même appel à Ajouter. TestCataloguesComplets (i18n_test.go) et
// TestClesGabaritsDeclarees (internal/web/i18n_test.go) refusent toute
// divergence — une clé utilisée dans un gabarit et absente d'un catalogue,
// ou l'inverse, fait échouer make check.
package i18n

import (
	"fmt"
	"strings"
)

// Lang est une langue d'interface supportée.
type Lang string

const (
	FR Lang = "fr"
	EN Lang = "en"

	// Defaut est la langue appliquée en l'absence de préférence explicite
	// (première visite, cookie absent ou invalide).
	Defaut = FR
)

// EstValide accepte "fr" ou "en" (insensible à la casse), sinon Defaut.
func EstValide(s string) (Lang, bool) {
	switch Lang(strings.ToLower(strings.TrimSpace(s))) {
	case FR:
		return FR, true
	case EN:
		return EN, true
	default:
		return Defaut, false
	}
}

// Autre renvoie l'autre langue supportée — sert au lien de bascule dans le
// menu, qui n'a jamais que deux états.
func (l Lang) Autre() Lang {
	if l == FR {
		return EN
	}
	return FR
}

// catalogueFR, catalogueEN portent les deux langues, remplies au démarrage
// par les init() des fichiers catalogue_*.go via Ajouter — jamais
// directement : la vérification anti-doublon n'aurait alors plus lieu.
var (
	catalogueFR = map[string]string{}
	catalogueEN = map[string]string{}
)

// Ajouter fusionne un lot de clés dans les deux catalogues. fr et en doivent
// porter exactement les mêmes clés (vérifié par TestCataloguesComplets, pas
// ici : ne pas coupler le démarrage du programme à l'exhaustivité d'un
// fichier de traduction encore en cours d'écriture). Panique si une clé est
// déjà posée par un autre fichier catalogue_*.go — un nom de clé choisi deux
// fois, presque toujours un copier-coller mal renommé.
func Ajouter(fr, en map[string]string) {
	for cle, valeur := range fr {
		if _, deja := catalogueFR[cle]; deja {
			panic(fmt.Sprintf("i18n: clé « %s » déjà déclarée (français)", cle))
		}
		catalogueFR[cle] = valeur
	}
	for cle, valeur := range en {
		if _, deja := catalogueEN[cle]; deja {
			panic(fmt.Sprintf("i18n: clé « %s » déjà déclarée (anglais)", cle))
		}
		catalogueEN[cle] = valeur
	}
}

func catalogue(lang Lang) map[string]string {
	if lang == EN {
		return catalogueEN
	}
	return catalogueFR
}

// T résout une clé dans la langue demandée. Une clé absente de cette langue
// retombe sur le français ; absente aussi du français, elle est rendue
// visible plutôt que silencieusement vide (« pas d'ambiguïté silencieuse »,
// CLAUDE.md) — un texte encadré de points d'exclamation ne passe pas
// inaperçu à la relecture d'un écran.
func T(lang Lang, cle string) string {
	if v, ok := catalogue(lang)[cle]; ok {
		return v
	}
	if v, ok := catalogueFR[cle]; ok {
		return v
	}
	return "!" + cle + "!"
}

// clesManquantes renvoie les clés présentes dans le français et absentes de
// l'anglais, et inversement. Utilisé par TestCataloguesComplets.
func clesManquantes() (absentesEN, absentesFR []string) {
	for cle := range catalogueFR {
		if _, ok := catalogueEN[cle]; !ok {
			absentesEN = append(absentesEN, cle)
		}
	}
	for cle := range catalogueEN {
		if _, ok := catalogueFR[cle]; !ok {
			absentesFR = append(absentesFR, cle)
		}
	}
	return absentesEN, absentesFR
}
