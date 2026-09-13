package sauvegarde

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"parallax/internal/db"
)

func baseDeTest(t *testing.T) *sql.DB {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "test.db")
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close() })
	if err := db.Migrer(base); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestEcrireLocaleCreeLeDossierEtNommeParHorodatage(t *testing.T) {
	base := baseDeTest(t)
	dossier := filepath.Join(t.TempDir(), "sauvegardes", "imbrique")

	nom, err := EcrireLocale(base, dossier)
	if err != nil {
		t.Fatal(err)
	}
	if !estFichierSauvegarde(nom) {
		t.Fatalf("nom hors convention : %q", nom)
	}
	if _, err := os.Stat(filepath.Join(dossier, nom)); err != nil {
		t.Fatalf("fichier attendu sur disque : %v", err)
	}
}

func TestEcrireLocaleDossierVide(t *testing.T) {
	base := baseDeTest(t)
	if _, err := EcrireLocale(base, ""); err == nil {
		t.Fatal("dossier vide : erreur attendue")
	}
}

func TestListerLocalesDossierAbsent(t *testing.T) {
	fichiers, err := ListerLocales(filepath.Join(t.TempDir(), "jamais-cree"))
	if err != nil || fichiers != nil {
		t.Fatalf("dossier absent : attendu (nil, nil), obtenu (%v, %v)", fichiers, err)
	}
}

func TestListerLocalesIgnoreLesFichiersEtrangers(t *testing.T) {
	dossier := t.TempDir()
	écrire := func(nom string) {
		if err := os.WriteFile(filepath.Join(dossier, nom), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	écrire("parallax-20260101-000000.db")
	écrire("notes.txt")
	écrire("parallax-20260102-000000.db.tmp")

	fichiers, err := ListerLocales(dossier)
	if err != nil {
		t.Fatal(err)
	}
	if len(fichiers) != 1 || fichiers[0].Nom != "parallax-20260101-000000.db" {
		t.Fatalf("attendu une seule sauvegarde reconnue, obtenu %+v", fichiers)
	}
}

func TestListerLocalesOrdreDuPlusRecent(t *testing.T) {
	dossier := t.TempDir()
	plusAncien := filepath.Join(dossier, "parallax-20260101-000000.db")
	plusRecent := filepath.Join(dossier, "parallax-20260102-000000.db")
	if err := os.WriteFile(plusAncien, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plusRecent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ancien := time.Now().Add(-48 * time.Hour)
	récent := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(plusAncien, ancien, ancien); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(plusRecent, récent, récent); err != nil {
		t.Fatal(err)
	}

	fichiers, err := ListerLocales(dossier)
	if err != nil {
		t.Fatal(err)
	}
	if len(fichiers) != 2 || fichiers[0].Nom != "parallax-20260102-000000.db" {
		t.Fatalf("attendu le plus récent en premier : %+v", fichiers)
	}
}

func TestPurgerLocalesSupprimeSeulementLesVieuxFichiersReconnus(t *testing.T) {
	dossier := t.TempDir()
	vieux := filepath.Join(dossier, "parallax-20250101-000000.db")
	récent := filepath.Join(dossier, "parallax-20260101-000000.db")
	étranger := filepath.Join(dossier, "notes.txt")
	for _, chemin := range []string{vieux, récent, étranger} {
		if err := os.WriteFile(chemin, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ancien := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(vieux, ancien, ancien); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(étranger, ancien, ancien); err != nil {
		t.Fatal(err)
	}

	n, err := PurgerLocales(dossier, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("attendu 1 fichier purgé, obtenu %d", n)
	}
	if _, err := os.Stat(vieux); !os.IsNotExist(err) {
		t.Fatal("le vieux fichier de sauvegarde aurait dû disparaître")
	}
	if _, err := os.Stat(récent); err != nil {
		t.Fatal("le fichier récent ne devait pas être touché")
	}
	if _, err := os.Stat(étranger); err != nil {
		t.Fatal("un fichier hors convention ne doit jamais être supprimé")
	}
}
