package depot

import (
	"errors"
	"testing"
)

func TestModeleNoeudUpsert(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	m := modeleDeTest(t, d)
	rev := revisionDeTest(t, d, m.ID)

	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, 6, false); err != nil {
		t.Fatalf("définition initiale : %v", err)
	}
	if n, err := d.LireNoeuds(rev.ID, r.TechnoElastic); err != nil || n != 6 {
		t.Fatalf("lecture : %v / %d", err, n)
	}

	// Deuxième appel : mise à jour, pas de doublon.
	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, 4, false); err != nil {
		t.Fatalf("mise à jour : %v", err)
	}
	if n, _ := d.LireNoeuds(rev.ID, r.TechnoElastic); n != 4 {
		t.Fatalf("mise à jour non prise en compte : %d", n)
	}

	if err := d.DefinirNoeuds(rev.ID, r.TechnoKafka, 6, false); err != nil {
		t.Fatalf("seconde techno : %v", err)
	}
	liste, err := d.ListerNoeuds(rev.ID)
	if err != nil {
		t.Fatalf("liste : %v", err)
	}
	if len(liste) != 2 {
		t.Fatalf("attendu 2 définitions, obtenu %+v", liste)
	}
}

func TestModeleNoeudValidation(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	m := modeleDeTest(t, d)
	rev := revisionDeTest(t, d, m.ID)

	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, -1, false); !errors.Is(err, ErrValidation) {
		t.Fatalf("nombre négatif : attendu ErrValidation, obtenu %v", err)
	}
	// 0 est licite (techno non installable sur ce modèle).
	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, 0, false); err != nil {
		t.Fatalf("zéro nœud devrait être accepté : %v", err)
	}
}

func TestModeleNoeudReferenceInconnue(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	m := modeleDeTest(t, d)
	rev := revisionDeTest(t, d, m.ID)

	if err := d.DefinirNoeuds(999, r.TechnoElastic, 6, false); !errors.Is(err, ErrReference) {
		t.Fatalf("révision inconnue : attendu ErrReference, obtenu %v", err)
	}
	if err := d.DefinirNoeuds(rev.ID, 999, 6, false); !errors.Is(err, ErrReference) {
		t.Fatalf("techno inconnue : attendu ErrReference, obtenu %v", err)
	}
}

func TestModeleNoeudIntrouvable(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	m := modeleDeTest(t, d)
	rev := revisionDeTest(t, d, m.ID)

	if _, err := d.LireNoeuds(rev.ID, r.TechnoElastic); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("couple non défini : attendu ErrIntrouvable, obtenu %v", err)
	}
}

// TestModeleNoeudImmuabilite couvre l'invariant 1 : le nombre de nœuds fait
// partie de la révision, donc figé dès qu'elle est référencée, sauf correction.
func TestModeleNoeudImmuabilite(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	m := modeleDeTest(t, d)
	rev := revisionDeTest(t, d, m.ID)

	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, 6, false); err != nil {
		t.Fatal(err)
	}

	referencerRevision(t, d, rev.ID)

	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, 4, false); !errors.Is(err, ErrImmuable) {
		t.Fatalf("attendu ErrImmuable, obtenu %v", err)
	}
	if n, _ := d.LireNoeuds(rev.ID, r.TechnoElastic); n != 6 {
		t.Fatalf("le refus ne doit rien modifier : %d", n)
	}

	if err := d.DefinirNoeuds(rev.ID, r.TechnoElastic, 4, true); err != nil {
		t.Fatalf("correction : %v", err)
	}
	if n, _ := d.LireNoeuds(rev.ID, r.TechnoElastic); n != 4 {
		t.Fatalf("correction non persistée : %d", n)
	}
}
