package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
)

func TestEcrireProduitUneArchiveZipValide(t *testing.T) {
	var buf bytes.Buffer
	err := Ecrire(&buf,
		Feuille{
			Nom:     "Vue",
			Entetes: []string{"Cluster", "Serveurs", "Disque utile (To)"},
			Lignes: [][]any{
				{"ElasticHot", 4, 362.5},
				{"ElasticCold1", 12, 4390.2},
				{"Kafka", nil, 0},
			},
		},
		Feuille{Nom: "Vide", Entetes: []string{"Rien"}, Lignes: nil},
	)
	if err != nil {
		t.Fatalf("écriture : %v", err)
	}

	lecteur, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("l'archive produite n'est pas un zip valide : %v", err)
	}

	attendus := []string{
		"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml",
		"xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml", "xl/worksheets/sheet2.xml",
	}
	presents := map[string]bool{}
	for _, f := range lecteur.File {
		presents[f.Name] = true
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("ouverture de %s : %v", f.Name, err)
		}
		// chaque partie doit être un XML bien formé : parcourir tous les
		// tokens doit se terminer par io.EOF, jamais une autre erreur.
		dec := xml.NewDecoder(rc)
		for {
			if _, err := dec.Token(); err != nil {
				if err.Error() != "EOF" {
					t.Fatalf("%s n'est pas un XML bien formé : %v", f.Name, err)
				}
				break
			}
		}
		rc.Close()
	}
	for _, a := range attendus {
		if !presents[a] {
			t.Fatalf("partie attendue absente de l'archive : %s", a)
		}
	}

	sheet1 := contenuPartie(t, lecteur, "xl/worksheets/sheet1.xml")
	for _, attendu := range []string{
		"ElasticHot", "ElasticCold1", "362.5", "4390.2", `r="A1"`, `r="C1"`,
	} {
		if !strings.Contains(sheet1, attendu) {
			t.Fatalf("sheet1.xml devrait contenir %q :\n%s", attendu, sheet1)
		}
	}
}

func TestEcrireEchappeLesCaracteresSpeciaux(t *testing.T) {
	var buf bytes.Buffer
	err := Ecrire(&buf, Feuille{
		Nom:     "T",
		Entetes: []string{"Nom"},
		Lignes:  [][]any{{`Cluster <prod> & "cold" 'v2'`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lecteur, _ := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	sheet := contenuPartie(t, lecteur, "xl/worksheets/sheet1.xml")

	// un XML mal échappé romprait le décodage : le vérifier explicitement
	dec := xml.NewDecoder(strings.NewReader(sheet))
	for {
		if _, err := dec.Token(); err != nil {
			break
		}
	}
	if strings.Contains(sheet, "<prod>") {
		t.Fatal("le contenu utilisateur n'a pas été échappé, XML invalide produit")
	}
}

func TestEcrireAucuneFeuilleEstUneErreur(t *testing.T) {
	var buf bytes.Buffer
	if err := Ecrire(&buf); err == nil {
		t.Fatal("attendu une erreur pour zéro feuille")
	}
}

func TestColonneLettres(t *testing.T) {
	cas := map[int]string{0: "A", 1: "B", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA"}
	for col, attendu := range cas {
		if obtenu := colonneLettres(col); obtenu != attendu {
			t.Fatalf("colonne %d : attendu %s, obtenu %s", col, attendu, obtenu)
		}
	}
}

func contenuPartie(t *testing.T, lecteur *zip.Reader, nom string) string {
	t.Helper()
	for _, f := range lecteur.File {
		if f.Name != nom {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		return buf.String()
	}
	t.Fatalf("partie introuvable : %s", nom)
	return ""
}
