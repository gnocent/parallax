// Package xlsx écrit des classeurs .xlsx minimaux : une ou plusieurs feuilles,
// cellules texte ou numériques, aucune mise en forme. Écrit à la main plutôt
// que via une bibliothèque tierce, dans l'esprit « un seul binaire autonome »
// du projet : le format OOXML d'une feuille de calcul simple est une poignée
// de fichiers XML dans une archive zip, largement à la portée d'un paquet de
// cette taille, sans tirer de dépendance lourde pour ce que le constructeur
// de vues exige (des tableaux croisés, pas des classeurs mis en forme).
//
// Ce que ce paquet ne fait PAS, volontairement : styles, formules, feuilles
// multiples avec mise en forme conditionnelle, graphiques. Si ces besoins
// apparaissent, c'est le signal qu'il faut réévaluer ce choix — pas ajouter
// des rustines ici.
package xlsx

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Feuille est une feuille de calcul : un nom d'onglet, une ligne d'en-têtes,
// et des lignes de données. Chaque cellule de Lignes doit être de type
// string, int, int64, float64, bool ou nil (cellule vide) ; tout autre type
// est converti via fmt.Sprint.
type Feuille struct {
	Nom     string
	Entetes []string
	Lignes  [][]any
}

// Ecrire produit un classeur .xlsx dans w, une feuille par Feuille fournie,
// dans l'ordre. Le nom de feuille est tronqué à 31 caractères et ses
// caractères interdits par Excel ([]:*?/\\) remplacés par « _ », comme le
// fait Excel lui-même à l'import.
func Ecrire(w io.Writer, feuilles ...Feuille) error {
	if len(feuilles) == 0 {
		return fmt.Errorf("écriture xlsx : aucune feuille fournie")
	}

	zw := zip.NewWriter(w)

	if err := ecrireFichier(zw, "[Content_Types].xml", contenuTypes(len(feuilles))); err != nil {
		return err
	}
	if err := ecrireFichier(zw, "_rels/.rels", relationsRacine); err != nil {
		return err
	}
	if err := ecrireFichier(zw, "xl/workbook.xml", contenuWorkbook(feuilles)); err != nil {
		return err
	}
	if err := ecrireFichier(zw, "xl/_rels/workbook.xml.rels", relationsWorkbook(len(feuilles))); err != nil {
		return err
	}
	for i, f := range feuilles {
		nom := fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		contenu, err := contenuFeuille(f)
		if err != nil {
			return fmt.Errorf("écriture xlsx : feuille %q : %w", f.Nom, err)
		}
		if err := ecrireFichier(zw, nom, contenu); err != nil {
			return err
		}
	}

	return zw.Close()
}

func ecrireFichier(zw *zip.Writer, nom, contenu string) error {
	fw, err := zw.Create(nom)
	if err != nil {
		return fmt.Errorf("écriture xlsx : création de %s : %w", nom, err)
	}
	if _, err := fw.Write([]byte(contenu)); err != nil {
		return fmt.Errorf("écriture xlsx : écriture de %s : %w", nom, err)
	}
	return nil
}

const enTeteXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

const relationsRacine = enTeteXML + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
	`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
	`</Relationships>`

func contenuTypes(nbFeuilles int) string {
	var b strings.Builder
	b.WriteString(enTeteXML)
	b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	for i := 1; i <= nbFeuilles; i++ {
		fmt.Fprintf(&b, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i)
	}
	b.WriteString(`</Types>`)
	return b.String()
}

func contenuWorkbook(feuilles []Feuille) string {
	var b strings.Builder
	b.WriteString(enTeteXML)
	b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
		`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, f := range feuilles {
		fmt.Fprintf(&b, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`,
			echapper(nomFeuilleValide(f.Nom)), i+1, i+1)
	}
	b.WriteString(`</sheets></workbook>`)
	return b.String()
}

func relationsWorkbook(nbFeuilles int) string {
	var b strings.Builder
	b.WriteString(enTeteXML)
	b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= nbFeuilles; i++ {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`,
			i, i)
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func contenuFeuille(f Feuille) (string, error) {
	var b strings.Builder
	b.WriteString(enTeteXML)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)

	numeroLigne := 1
	if len(f.Entetes) > 0 {
		if err := ecrireLigne(&b, numeroLigne, entetesEnValeurs(f.Entetes)); err != nil {
			return "", err
		}
		numeroLigne++
	}
	for _, ligne := range f.Lignes {
		if err := ecrireLigne(&b, numeroLigne, ligne); err != nil {
			return "", err
		}
		numeroLigne++
	}

	b.WriteString(`</sheetData></worksheet>`)
	return b.String(), nil
}

func entetesEnValeurs(entetes []string) []any {
	out := make([]any, len(entetes))
	for i, e := range entetes {
		out[i] = e
	}
	return out
}

func ecrireLigne(b *strings.Builder, numeroLigne int, valeurs []any) error {
	fmt.Fprintf(b, `<row r="%d">`, numeroLigne)
	for col, v := range valeurs {
		ref := referenceCellule(col, numeroLigne)
		if err := ecrireCellule(b, ref, v); err != nil {
			return err
		}
	}
	b.WriteString(`</row>`)
	return nil
}

func ecrireCellule(b *strings.Builder, ref string, v any) error {
	switch val := v.(type) {
	case nil:
		// cellule absente : Excel traite une ligne sans <c> à cette colonne
		// comme vide, pas besoin d'émettre quoi que ce soit.
	case string:
		fmt.Fprintf(b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, echapper(val))
	case bool:
		texte := "non"
		if val {
			texte = "oui"
		}
		fmt.Fprintf(b, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, texte)
	case int:
		fmt.Fprintf(b, `<c r="%s"><v>%d</v></c>`, ref, val)
	case int64:
		fmt.Fprintf(b, `<c r="%s"><v>%d</v></c>`, ref, val)
	case float64:
		fmt.Fprintf(b, `<c r="%s"><v>%s</v></c>`, ref, strconv.FormatFloat(val, 'g', -1, 64))
	case fmt.Stringer:
		fmt.Fprintf(b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, echapper(val.String()))
	default:
		fmt.Fprintf(b, `<c r="%s" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, echapper(fmt.Sprint(val)))
	}
	return nil
}

// referenceCellule convertit (colonne 0-indexée, ligne 1-indexée) en
// référence Excel (0,1) -> "A1", (27,3) -> "AB3".
func referenceCellule(col, ligne int) string {
	return colonneLettres(col) + strconv.Itoa(ligne)
}

func colonneLettres(col int) string {
	var lettres []byte
	for col >= 0 {
		lettres = append([]byte{byte('A' + col%26)}, lettres...)
		col = col/26 - 1
	}
	return string(lettres)
}

func echapper(s string) string {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		// xml.EscapeText n'échoue que si b.Write échoue ; strings.Builder
		// n'échoue jamais.
		return s
	}
	return b.String()
}

// nomFeuilleValide applique les contraintes Excel sur le nom d'un onglet :
// 31 caractères maximum, et aucun des caractères []:*?/\ autorisés.
func nomFeuilleValide(nom string) string {
	nom = strings.NewReplacer("[", "_", "]", "_", ":", "_", "*", "_", "?", "_", "/", "_", "\\", "_").Replace(nom)
	if len(nom) > 31 {
		nom = nom[:31]
	}
	if nom == "" {
		nom = "Feuille1"
	}
	return nom
}
