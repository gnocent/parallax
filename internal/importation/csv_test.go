package importation

import (
	"strings"
	"testing"
)

func TestLireCSV(t *testing.T) {
	source := "code;libelle;annee\nSTD-2020;STD 2020;2020\nDENSE-2025;DENSE 2025;2025\n"
	entetes, lignes, numeros, err := LireCSV(strings.NewReader(source))
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if len(entetes) != 3 || entetes[0] != "code" {
		t.Fatalf("en-têtes inattendus : %v", entetes)
	}
	if len(lignes) != 2 {
		t.Fatalf("attendu 2 lignes, obtenu %d", len(lignes))
	}
	if lignes[0]["code"] != "STD-2020" || lignes[0]["annee"] != "2020" {
		t.Fatalf("première ligne inattendue : %+v", lignes[0])
	}
	if numeros[0] != 2 || numeros[1] != 3 {
		t.Fatalf("numéros de ligne attendus [2 3], obtenu %v", numeros)
	}
}

func TestLireCSVAvecBOM(t *testing.T) {
	source := "\xEF\xBB\xBFcode;libelle\nSTD-2020;STD\n"
	entetes, lignes, _, err := LireCSV(strings.NewReader(source))
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if entetes[0] != "code" {
		t.Fatalf("la BOM aurait dû être retirée de l'en-tête, obtenu %q", entetes[0])
	}
	if lignes[0]["code"] != "STD-2020" {
		t.Fatalf("première cellule inattendue : %+v", lignes[0])
	}
}

func TestLireCSVColonneFacultativeAbsenteEnFinDeLigne(t *testing.T) {
	source := "code;libelle;site\nDC1;Zone 1\n"
	_, lignes, _, err := LireCSV(strings.NewReader(source))
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if lignes[0]["site"] != "" {
		t.Fatalf("colonne manquante en fin de ligne : attendu vide, obtenu %q", lignes[0]["site"])
	}
}

func TestEntetesManquantes(t *testing.T) {
	manquantes := EntetesManquantes([]string{"code", "libelle"}, []string{"code", "annee"})
	if len(manquantes) != 1 || manquantes[0] != "annee" {
		t.Fatalf("attendu [annee], obtenu %v", manquantes)
	}
}

func TestParserEntier(t *testing.T) {
	if v, err := ParserEntier(""); err != nil || v != nil {
		t.Fatalf("vide : attendu nil sans erreur, obtenu %v / %v", v, err)
	}
	v, err := ParserEntier("2025")
	if err != nil || v == nil || *v != 2025 {
		t.Fatalf("attendu 2025, obtenu %v / %v", v, err)
	}
	if _, err := ParserEntier("abc"); err == nil {
		t.Fatal("attendu une erreur pour une valeur non numérique")
	}
}

func TestParserDecimalVirguleEtPoint(t *testing.T) {
	v1, err := ParserDecimal("42,5")
	if err != nil || v1 == nil || *v1 != 42.5 {
		t.Fatalf("virgule : attendu 42.5, obtenu %v / %v", v1, err)
	}
	v2, err := ParserDecimal("42.5")
	if err != nil || v2 == nil || *v2 != 42.5 {
		t.Fatalf("point : attendu 42.5, obtenu %v / %v", v2, err)
	}
}
