package web

import (
	"fmt"
	"strings"

	"parallax/internal/depot"
)

// gabarit.go est le moteur du gabarit de description des demandes de
// matériel (backlog v3.2) : un texte libre, avec retours à la ligne, où
// {nom} désigne une variable du catalogue VariablesGabarit, {{ une accolade
// ouvrante littérale et }} une accolade fermante littérale. Volontairement
// minuscule — pas de condition, pas de boucle, pas de format : l'outil de
// demande interne attend quelques lignes de texte, et le rédacteur doit
// pouvoir prédire le résultat en lisant le gabarit.
//
// La validation est faite à la saisie (CompilerGabarit), jamais au rendu :
// une variable inconnue ou une accolade non fermée est refusée en la
// nommant, et le rendu d'un gabarit compilé ne peut plus échouer. Une
// valeur absente est rendue vide, comme le veut le backlog : un serveur
// hypothétique sans IP produit un bloc à trou, qui se complète quand l'IP
// arrive.

// VariableGabarit est une entrée du catalogue : le nom entre accolades et
// le libellé de l'aide-mémoire affiché à l'écran d'édition.
type VariableGabarit struct {
	Nom     string
	Libelle string
}

// VariablesGabarit est le catalogue ordonné des variables admises, dans
// l'ordre du backlog v3.2. Les clés doivent correspondre exactement à celles
// de depot.FicheDemande.Valeurs — TestVariablesGabaritCouvrentLaFiche le
// garantit.
var VariablesGabarit = []VariableGabarit{
	{"num_fiche", "Numéro de fiche (réf. serveur dans la demande)"},
	{"num_demande", "Numéro de demande"},
	{"nom_physique", "Nom physique"},
	{"hostname", "Hostname"},
	{"ip", "Adresse IP"},
	{"vlan", "Code du VLAN"},
	{"zone", "Code de la zone"},
	{"site", "Site de la zone"},
	{"position", "Position dans la zone"},
	{"typologie", "Typologie (P, A, D, PA, AD, PAD)"},
	{"code_appli", "Code application"},
	{"commentaire", "Commentaire du serveur"},
	{"projet", "Code du projet (cluster affecté)"},
	{"environnement", "Code de l'environnement"},
	{"techno", "Code de la techno"},
	{"tier", "Code du tier"},
	{"usage", "Code de l'usage fonctionnel"},
	{"cluster", "Nom du cluster"},
	{"modele", "Code du modèle (révision rattachée)"},
	{"modele_type", "Type du modèle"},
	{"modele_description", "Description du modèle"},
	{"revision", "Numéro de révision"},
	{"cpu", "Cœurs CPU (total de la révision)"},
	{"ram", "RAM en Go (total)"},
	{"hdd", "HDD en To (total)"},
	{"ssd", "SSD en To (total)"},
	{"nic", "Réseau en Gbps (total)"},
	{"gpu", "Nombre de GPU"},
	{"statut", "Statut du serveur"},
	{"date_entree", "Date d'entrée"},
	{"scenario", "Nom du scénario (vide pour le réel)"},
}

// variablesConnues indexe le catalogue pour la validation.
var variablesConnues = func() map[string]bool {
	m := make(map[string]bool, len(VariablesGabarit))
	for _, v := range VariablesGabarit {
		m[v.Nom] = true
	}
	return m
}()

// segment est un morceau de gabarit compilé : soit du texte à recopier tel
// quel (accolades littérales déjà dépliées), soit le nom d'une variable.
type segment struct {
	texte    string
	variable bool
}

// Gabarit est un gabarit compilé, prêt à rendre autant de fiches qu'on veut
// sans revalider.
type Gabarit struct {
	segments []segment
}

// CompilerGabarit analyse le texte et refuse, avec depot.ErrValidation, une
// variable hors catalogue (nommée dans le message), une accolade ouvrante
// jamais fermée, ou une accolade fermante isolée — cette dernière n'est pas
// ambiguë en soi, mais un « } » oublié dans un texte collé est presque
// toujours une faute de frappe, et la règle « {{ et }} pour un littéral »
// se retient mieux si elle est symétrique. Les positions sont comptées en
// caractères depuis le début du texte, à partir de 1.
func CompilerGabarit(texte string) (*Gabarit, error) {
	runes := []rune(texte)
	g := &Gabarit{}
	var courant strings.Builder
	viderTexte := func() {
		if courant.Len() > 0 {
			g.segments = append(g.segments, segment{texte: courant.String()})
			courant.Reset()
		}
	}

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch c {
		case '{':
			if i+1 < len(runes) && runes[i+1] == '{' {
				courant.WriteRune('{')
				i++
				continue
			}
			fin := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '}' {
					fin = j
					break
				}
				// une variable ne contient ni retour à la ligne ni
				// accolade : l'accolade ouvrante est alors orpheline.
				if runes[j] == '\n' || runes[j] == '{' {
					break
				}
			}
			if fin < 0 {
				return nil, fmt.Errorf("%w : accolade ouverte au caractère %d jamais fermée (écrire {{ pour une accolade littérale)",
					depot.ErrValidation, i+1)
			}
			nom := strings.TrimSpace(string(runes[i+1 : fin]))
			if nom == "" {
				return nil, fmt.Errorf("%w : variable vide « {} » au caractère %d", depot.ErrValidation, i+1)
			}
			if !variablesConnues[nom] {
				return nil, fmt.Errorf("%w : variable inconnue « %s » au caractère %d", depot.ErrValidation, nom, i+1)
			}
			viderTexte()
			g.segments = append(g.segments, segment{texte: nom, variable: true})
			i = fin
		case '}':
			if i+1 < len(runes) && runes[i+1] == '}' {
				courant.WriteRune('}')
				i++
				continue
			}
			return nil, fmt.Errorf("%w : accolade fermante isolée au caractère %d (écrire }} pour une accolade littérale)",
				depot.ErrValidation, i+1)
		default:
			courant.WriteRune(c)
		}
	}
	viderTexte()
	return g, nil
}

// Rendre substitue chaque variable par sa valeur ; une clé absente de la
// carte rend vide. Le texte libre, retours à la ligne compris, est recopié
// tel quel.
func (g *Gabarit) Rendre(valeurs map[string]string) string {
	var b strings.Builder
	for _, s := range g.segments {
		if s.variable {
			b.WriteString(valeurs[s.texte])
		} else {
			b.WriteString(s.texte)
		}
	}
	return b.String()
}

// Variables renvoie, dans l'ordre d'apparition et sans doublon, les
// variables utilisées par le gabarit — pour l'aperçu de l'écran d'édition.
func (g *Gabarit) Variables() []string {
	vus := map[string]bool{}
	var out []string
	for _, s := range g.segments {
		if s.variable && !vus[s.texte] {
			vus[s.texte] = true
			out = append(out, s.texte)
		}
	}
	return out
}
