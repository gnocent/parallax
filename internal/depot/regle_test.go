package depot

import (
	"errors"
	"testing"

	"parallax/internal/capacity"
)

func semerRegleContexte(t *testing.T, d *Depot) (refs refs, metrique int64, hot, cold int64) {
	t.Helper()
	r := semerRefs(t, d.base)
	m, err := d.CreerMetrique(Metrique{Code: "DISQUE_UTILE_TO", Libelle: "Disque", Unite: "TO"})
	if err != nil {
		t.Fatal(err)
	}
	resHot, _ := d.base.Exec(
		`INSERT INTO cluster (nom, projet_id, environnement_id, techno_id, tier_id)
		 VALUES ('ElasticHot', ?, ?, ?, ?)`, r.Projet, r.EnvProd, r.TechnoElastic, r.TierHot)
	resCold, _ := d.base.Exec(
		`INSERT INTO cluster (nom, projet_id, environnement_id, techno_id, tier_id)
		 VALUES ('ElasticCold', ?, ?, ?, ?)`, r.Projet, r.EnvProd, r.TechnoElastic, r.TierCold)
	h, _ := resHot.LastInsertId()
	c, _ := resCold.LastInsertId()
	return r, m.ID, h, c
}

func TestRegleValidationExpression(t *testing.T) {
	d := depotDeTest(t)
	_, m, _, _ := semerRegleContexte(t, d)

	_, err := d.CreerRegle(Regle{
		Nom: "cassée", MetriqueID: m, Expression: "debit * ", // syntaxe invalide
		NiveauEvaluation: capacity.NiveauPerimetre,
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expression invalide : attendu ErrValidation, obtenu %v", err)
	}

	_, err = d.CreerRegle(Regle{
		Nom: "niveau inconnu", MetriqueID: m, Expression: "1",
		NiveauEvaluation: "AUTRE",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("niveau inconnu : attendu ErrValidation, obtenu %v", err)
	}
}

func TestRegleRefusRecouvrement(t *testing.T) {
	d := depotDeTest(t)
	r, m, _, _ := semerRegleContexte(t, d)

	// règle spécifique au tier HOT
	if _, err := d.CreerRegle(Regle{
		Nom: "Disque Elastic HOT", MetriqueID: m,
		Expression: "debit_jour_to * retention_jours", NiveauEvaluation: capacity.NiveauPerimetre,
		ComposantOffre: ptrS("ssd"),
		TechnoID:       i64(r.TechnoElastic), TierID: i64(r.TierHot),
	}); err != nil {
		t.Fatalf("règle HOT : %v", err)
	}

	// règle générique Elastic (tous tiers) : recouvre la précédente sur le cluster HOT
	_, err := d.CreerRegle(Regle{
		Nom: "Disque Elastic générique", MetriqueID: m,
		Expression: "debit_jour_to", NiveauEvaluation: capacity.NiveauPerimetre,
		ComposantOffre: ptrS("ssd"), TechnoID: i64(r.TechnoElastic),
	})
	if !errors.Is(err, ErrInvariant) {
		t.Fatalf("recouvrement : attendu ErrInvariant, obtenu %v", err)
	}

	// la règle refusée ne doit pas subsister (tx annulée)
	regles, _ := d.ListerRegles(m)
	if len(regles) != 1 {
		t.Fatalf("la règle en conflit a été insérée puis non annulée : %d règles", len(regles))
	}
	hot := regles[0].ID

	// en désactivant la règle HOT, la générique peut être créée
	if err := d.DesactiverRegle(hot); err != nil {
		t.Fatal(err)
	}
	gen, err := d.CreerRegle(Regle{
		Nom: "Disque Elastic générique", MetriqueID: m, Expression: "debit_jour_to",
		NiveauEvaluation: capacity.NiveauPerimetre, ComposantOffre: ptrS("ssd"),
		TechnoID: i64(r.TechnoElastic),
	})
	if err != nil {
		t.Fatalf("règle générique après désactivation de HOT : %v", err)
	}
	_ = gen

	// ...mais réactiver HOT est alors refusé : la générique active la recouvre
	if err := d.ActiverRegle(hot); !errors.Is(err, ErrInvariant) {
		t.Fatalf("réactivation en conflit : attendu ErrInvariant, obtenu %v", err)
	}
}

func TestRegleDuplication(t *testing.T) {
	d := depotDeTest(t)
	r, m, _, _ := semerRegleContexte(t, d)

	src, err := d.CreerRegle(Regle{
		Nom: "Disque Elastic HOT 2026", MetriqueID: m, Expression: "debit_jour_to",
		NiveauEvaluation: capacity.NiveauPerimetre, ComposantOffre: ptrS("ssd"),
		TechnoID: i64(r.TechnoElastic), TierID: i64(r.TierHot),
	})
	if err != nil {
		t.Fatal(err)
	}
	copie, err := d.DupliquerRegle(src.ID)
	if err != nil {
		t.Fatalf("duplication : %v", err)
	}
	if copie.Actif {
		t.Fatal("la copie doit être inactive")
	}
	if copie.Nom != "Disque Elastic HOT 2026 (copie)" {
		t.Fatalf("nom de copie inattendu : %s", copie.Nom)
	}
}

func ptrS(s string) *string { return &s }
