package depot

import (
	"errors"
	"testing"
)

func TestRevisionNumerotationAuto(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)

	r1, err := d.CreerRevision(Revision{ModeleID: m.ID, DateEffet: "2025-01-15", Libelle: ptrDe("initiale")})
	if err != nil {
		t.Fatalf("création r1 : %v", err)
	}
	if r1.Numero != 1 {
		t.Fatalf("première révision : numéro %d attendu 1", r1.Numero)
	}

	r2, err := d.CreerRevision(Revision{ModeleID: m.ID, DateEffet: "2025-06-01", Libelle: ptrDe("ajout 8 SSD")})
	if err != nil {
		t.Fatalf("création r2 : %v", err)
	}
	if r2.Numero != 2 {
		t.Fatalf("deuxième révision : numéro %d attendu 2", r2.Numero)
	}

	// La numérotation est propre à chaque modèle.
	autre, err := d.CreerModele(Modele{Type: "ECO", Annee: 2025, Code: "ECO-2025"})
	if err != nil {
		t.Fatal(err)
	}
	rAutre, err := d.CreerRevision(Revision{ModeleID: autre.ID, DateEffet: "2025-03-01"})
	if err != nil {
		t.Fatalf("création révision autre modèle : %v", err)
	}
	if rAutre.Numero != 1 {
		t.Fatalf("numérotation non isolée par modèle : %d", rAutre.Numero)
	}

	liste, err := d.ListerRevisions(m.ID)
	if err != nil {
		t.Fatalf("liste : %v", err)
	}
	if len(liste) != 2 || liste[0].Numero != 1 || liste[1].Numero != 2 {
		t.Fatalf("liste non triée par numéro : %+v", liste)
	}

	relu, err := d.LireRevision(r2.ID)
	if err != nil || relu.Libelle == nil || *relu.Libelle != "ajout 8 SSD" {
		t.Fatalf("relecture : %v / %+v", err, relu)
	}
}

func TestRevisionModeleInconnu(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.CreerRevision(Revision{ModeleID: 999, DateEffet: "2025-01-01"}); !errors.Is(err, ErrReference) {
		t.Fatalf("modèle inconnu : attendu ErrReference, obtenu %v", err)
	}
}

func TestRevisionDateInvalide(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	if _, err := d.CreerRevision(Revision{ModeleID: m.ID, DateEffet: "2025/01/01"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("date invalide : attendu ErrValidation, obtenu %v", err)
	}
}

func TestRevisionIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	if _, err := d.LireRevision(404); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("lecture : attendu ErrIntrouvable, obtenu %v", err)
	}
	if _, err := d.ModifierRevision(Revision{ID: 404, DateEffet: "2025-01-01"}, false); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification : attendu ErrIntrouvable, obtenu %v", err)
	}
}

// TestRevisionImmuabilite couvre l'invariant 1 : une révision non référencée se
// modifie librement ; référencée, elle refuse toute modification hors mode
// correction ; en mode correction, la modification passe et signale par
// avertissement qu'un historique a été écrasé.
func TestRevisionImmuabilite(t *testing.T) {
	d := depotDeTest(t)
	m := modeleDeTest(t, d)
	r := revisionDeTest(t, d, m.ID)

	// Non référencée : modification libre, sans avertissement.
	r.Libelle = ptrDe("libellé corrigé avant rattachement")
	avert, err := d.ModifierRevision(r, false)
	if err != nil {
		t.Fatalf("modification d'une révision non référencée : %v", err)
	}
	if avert {
		t.Fatal("aucun avertissement attendu sur une révision non référencée")
	}

	if ref, _ := d.RevisionEstReferencee(r.ID); ref {
		t.Fatal("la révision ne devrait pas encore être référencée")
	}
	referencerRevision(t, d, r.ID)
	if ref, _ := d.RevisionEstReferencee(r.ID); !ref {
		t.Fatal("la révision devrait être référencée")
	}

	// Référencée, correction == false : refus, aucune écriture.
	r.Libelle = ptrDe("tentative interdite")
	if _, err := d.ModifierRevision(r, false); !errors.Is(err, ErrImmuable) {
		t.Fatalf("attendu ErrImmuable, obtenu %v", err)
	}
	relu, _ := d.LireRevision(r.ID)
	if relu.Libelle == nil || *relu.Libelle != "libellé corrigé avant rattachement" {
		t.Fatalf("le refus ne doit rien modifier : %+v", relu)
	}

	// Référencée, correction == true : passe, avertissement == true.
	r.Libelle = ptrDe("correction d'une coquille")
	r.Commentaire = ptrDe("faute de frappe sur la date")
	avert, err = d.ModifierRevision(r, true)
	if err != nil {
		t.Fatalf("correction : %v", err)
	}
	if !avert {
		t.Fatal("avertissement attendu : une révision référencée a été écrasée")
	}
	relu, _ = d.LireRevision(r.ID)
	if relu.Libelle == nil || *relu.Libelle != "correction d'une coquille" {
		t.Fatalf("correction non persistée : %+v", relu)
	}
}
