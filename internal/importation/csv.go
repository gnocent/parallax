// Package importation porte la reprise des fichiers existants (v1.4 du
// backlog) : modèles puis serveurs, par fichier CSV. Les Excel actuels sont
// adaptés pour produire ce format — ce paquet ne lit
// pas de classeurs Excel directement, seulement du CSV, pour rester simple
// et sans dépendance.
//
// Convention commune aux deux imports (modèles, serveurs) :
//
//   - séparateur point-virgule, pas virgule : un CSV Excel en locale FR
//     utilise la virgule comme séparateur décimal, la virgule ne peut donc
//     pas aussi séparer les colonnes
//   - première ligne = en-têtes, dans n'importe quel ordre ; les colonnes
//     inconnues sont ignorées, les colonnes obligatoires absentes sont une
//     erreur immédiate (avant même de lire les lignes)
//   - une ligne invalide n'empêche pas de continuer à valider les suivantes :
//     le rapport liste toutes les anomalies en une passe, pas une seule à la
//     fois au fil de plusieurs essais
//   - mode simulation ou réel : dans les deux cas, TOUTES les lignes sont
//     validées d'abord ; le réel n'écrit que si zéro erreur, dans une seule
//     transaction — jamais d'import partiel
package importation

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ErreurLigne signale une anomalie sur une ligne précise du fichier importé.
// Ligne compte depuis 1 en incluant l'en-tête (ligne 1 = en-têtes, ligne 2 =
// première donnée), pour correspondre à ce que l'utilisateur voit en ouvrant
// le fichier dans un tableur.
type ErreurLigne struct {
	Ligne   int
	Message string
}

// Rapport résume une simulation ou un import réel. Ecrit ne vaut jamais true
// s'il reste des erreurs : c'est l'invariant « aucun import partiel ».
type Rapport struct {
	Simulation bool
	NbLignes   int
	Erreurs    []ErreurLigne
	Ecrit      bool
	NbCreees   int
}

// EnErreur indique si au moins une ligne est invalide.
func (r Rapport) EnErreur() bool { return len(r.Erreurs) > 0 }

// LireCSV décode un CSV point-virgule avec en-tête et renvoie une ligne de
// données par élément, sous forme de carte en-tête -> valeur (valeurs
// espaces de tête/fin retirées). Une BOM UTF-8 éventuelle en tête de fichier
// est ignorée (Excel en ajoute une systématiquement à l'export CSV).
//
// Le numéro de ligne renvoyé à côté de chaque carte suit la convention de
// ErreurLigne (l'en-tête compte pour la ligne 1).
func LireCSV(r io.Reader) (entetes []string, lignes []map[string]string, numeros []int, err error) {
	lecteur := csv.NewReader(retirerBOM(r))
	lecteur.Comma = ';'
	lecteur.TrimLeadingSpace = true
	lecteur.FieldsPerRecord = -1 // tolère des lignes plus courtes (colonnes facultatives vides en fin)

	brut, err := lecteur.ReadAll()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lecture du CSV : %w", err)
	}
	if len(brut) == 0 {
		return nil, nil, nil, fmt.Errorf("fichier vide")
	}

	entetes = make([]string, len(brut[0]))
	for i, e := range brut[0] {
		entetes[i] = strings.TrimSpace(e)
	}

	for i, ligne := range brut[1:] {
		m := make(map[string]string, len(entetes))
		for j, entete := range entetes {
			if j < len(ligne) {
				m[entete] = strings.TrimSpace(ligne[j])
			} else {
				m[entete] = ""
			}
		}
		lignes = append(lignes, m)
		numeros = append(numeros, i+2) // +1 pour l'en-tête, +1 pour l'indexation à 1
	}
	return entetes, lignes, numeros, nil
}

// retirerBOM saute les trois octets de la BOM UTF-8 si le flux commence par
// elle, sans consommer d'octets sinon (bufio.Reader.Peek n'avance pas le
// curseur de lecture).
func retirerBOM(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	premiers, err := br.Peek(3)
	if err == nil && premiers[0] == 0xEF && premiers[1] == 0xBB && premiers[2] == 0xBF {
		br.Discard(3)
	}
	return br
}

// EntetesManquantes renvoie, parmi obligatoires, celles absentes de entetes.
func EntetesManquantes(entetes []string, obligatoires []string) []string {
	presentes := map[string]bool{}
	for _, e := range entetes {
		presentes[e] = true
	}
	var manquantes []string
	for _, o := range obligatoires {
		if !presentes[o] {
			manquantes = append(manquantes, o)
		}
	}
	return manquantes
}

// ---------------------------------------------------------------- nombres

// ParserEntier lit un entier depuis une valeur de cellule, vide autorisé
// (renvoie nil) si la colonne est facultative.
func ParserEntier(valeur string) (*int64, error) {
	if valeur == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(valeur, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("« %s » n'est pas un entier", valeur)
	}
	return &v, nil
}

// ParserDecimal lit un nombre à virgule, en acceptant la virgule ou le point
// comme séparateur décimal (les deux se rencontrent selon l'origine du
// fichier). Vide autorisé (renvoie nil).
func ParserDecimal(valeur string) (*float64, error) {
	if valeur == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(valeur, ",", "."), 64)
	if err != nil {
		return nil, fmt.Errorf("« %s » n'est pas un nombre", valeur)
	}
	return &v, nil
}
