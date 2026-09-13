package depot

import (
	"errors"
	"testing"
)

func TestLicenceContratCRUD(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)

	cree, err := d.CreerLicenceContrat(LicenceContrat{
		TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: "ram", Niveau: "machine",
		RamMaxGo: f64(256), CoutUnitaireHT: f64(1000),
	})
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if cree.ID == 0 || cree.Mecanisme != MecanismeLicenceRam || cree.Niveau != NiveauLicenceMachine {
		t.Fatalf("contrat créé inattendu (mécanisme et niveau normalisés en majuscules) : %+v", cree)
	}

	lu, err := d.LireLicenceContrat(cree.ID)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if lu.TechnoID != r.TechnoElastic || lu.Annee != 2025 || lu.ScenarioID != nil ||
		lu.RamMaxGo == nil || *lu.RamMaxGo != 256 || lu.CoutUnitaireHT == nil || *lu.CoutUnitaireHT != 1000 {
		t.Fatalf("contrat relu inattendu : %+v", lu)
	}

	// doublon (techno, année, réel) refusé par l'index unique
	if _, err := d.CreerLicenceContrat(LicenceContrat{
		TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: MecanismeLicenceNoeuds, Niveau: NiveauLicenceGlobal,
	}); !errors.Is(err, ErrConflit) {
		t.Fatalf("doublon : attendu ErrConflit, obtenu %v", err)
	}

	// modification : passage en NOEUDS, la RAM max est effacée (sans objet)
	lu.Mecanisme = MecanismeLicenceNoeuds
	lu.Annee = 2026
	if err := d.ModifierLicenceContrat(lu); err != nil {
		t.Fatalf("modification : %v", err)
	}
	relu, _ := d.LireLicenceContrat(cree.ID)
	if relu.Annee != 2026 || relu.Mecanisme != MecanismeLicenceNoeuds || relu.RamMaxGo != nil {
		t.Fatalf("contrat modifié inattendu : %+v", relu)
	}

	liste, err := d.ListerLicenceContrats(nil)
	if err != nil {
		t.Fatalf("liste : %v", err)
	}
	if len(liste) != 1 || liste[0].ID != cree.ID {
		t.Fatalf("liste inattendue : %+v", liste)
	}

	if err := d.SupprimerLicenceContrat(cree.ID); err != nil {
		t.Fatalf("suppression : %v", err)
	}
	if _, err := d.LireLicenceContrat(cree.ID); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("après suppression : attendu ErrIntrouvable, obtenu %v", err)
	}
	if err := d.ModifierLicenceContrat(relu); !errors.Is(err, ErrIntrouvable) {
		t.Fatalf("modification d'un contrat supprimé : attendu ErrIntrouvable, obtenu %v", err)
	}
}

func TestLicenceContratValidation(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)

	cas := []struct {
		nom string
		c   LicenceContrat
	}{
		{"techno absente", LicenceContrat{Annee: 2025, Mecanisme: MecanismeLicenceNoeuds, Niveau: NiveauLicenceMachine}},
		{"année hors plage", LicenceContrat{TechnoID: r.TechnoElastic, Annee: 1999, Mecanisme: MecanismeLicenceNoeuds, Niveau: NiveauLicenceMachine}},
		{"mécanisme inconnu", LicenceContrat{TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: "CPU", Niveau: NiveauLicenceMachine}},
		{"niveau inconnu", LicenceContrat{TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: MecanismeLicenceNoeuds, Niveau: "PARC"}},
		{"RAM max absente pour RAM", LicenceContrat{TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: MecanismeLicenceRam, Niveau: NiveauLicenceMachine}},
		{"RAM max nulle pour MAX_NOEUDS_RAM", LicenceContrat{TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: MecanismeLicenceMaxNoeudsRam, Niveau: NiveauLicenceMachine, RamMaxGo: f64(0)}},
		{"coût négatif", LicenceContrat{TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: MecanismeLicenceNoeuds, Niveau: NiveauLicenceMachine, CoutUnitaireHT: f64(-1)}},
	}
	for _, c := range cas {
		if _, err := d.CreerLicenceContrat(c.c); !errors.Is(err, ErrValidation) {
			t.Errorf("%s : attendu ErrValidation, obtenu %v", c.nom, err)
		}
	}

	// NOEUDS sans RAM max : accepté, RAM max sans objet
	if _, err := d.CreerLicenceContrat(LicenceContrat{
		TechnoID: r.TechnoElastic, Annee: 2025, Mecanisme: MecanismeLicenceNoeuds, Niveau: NiveauLicenceMachine,
	}); err != nil {
		t.Fatalf("NOEUDS sans RAM max devrait passer : %v", err)
	}

	// techno inexistante : clé étrangère
	if _, err := d.CreerLicenceContrat(LicenceContrat{
		TechnoID: 999, Annee: 2025, Mecanisme: MecanismeLicenceNoeuds, Niveau: NiveauLicenceMachine,
	}); !errors.Is(err, ErrReference) {
		t.Fatalf("techno inexistante : attendu ErrReference, obtenu %v", err)
	}
}

// TestResoudreContrats couvre la règle de résolution : année exacte, à
// défaut la plus récente antérieure, scénario avant réel, et une techno sans
// contrat absente de la carte.
func TestResoudreContrats(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	exec(t, d.base, `INSERT INTO scenario (id, nom, description, statut, date_creation)
		VALUES (1, 'Hypothèse', 'test', 'ACTIF', '2026-01-01')`)
	sc := int64(1)

	creer := func(techno int64, annee int, scenario *int64, mecanisme string, ram float64) LicenceContrat {
		t.Helper()
		var ramMax *float64
		if mecanisme != MecanismeLicenceNoeuds {
			ramMax = f64(ram)
		}
		c, err := d.CreerLicenceContrat(LicenceContrat{
			TechnoID: techno, Annee: annee, ScenarioID: scenario,
			Mecanisme: mecanisme, Niveau: NiveauLicenceMachine, RamMaxGo: ramMax,
		})
		if err != nil {
			t.Fatalf("fixture contrat %d/%d : %v", techno, annee, err)
		}
		return c
	}
	elastic2024 := creer(r.TechnoElastic, 2024, nil, MecanismeLicenceNoeuds, 0)
	elastic2026 := creer(r.TechnoElastic, 2026, nil, MecanismeLicenceRam, 256)
	elastic2025Sc := creer(r.TechnoElastic, 2025, &sc, MecanismeLicenceMaxNoeudsRam, 512)
	// Kafka : aucun contrat

	// réel, 2024 : le contrat de l'année
	res, err := d.ResoudreContrats(2024, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res[r.TechnoElastic].ID != elastic2024.ID {
		t.Fatalf("2024 réel : attendu le contrat 2024, obtenu %+v", res[r.TechnoElastic])
	}
	if _, ok := res[r.TechnoKafka]; ok {
		t.Fatal("Kafka sans contrat ne doit pas figurer dans la carte")
	}

	// réel, 2025 : pas de contrat 2025, le 2024 court jusqu'au suivant
	res, _ = d.ResoudreContrats(2025, nil)
	if res[r.TechnoElastic].ID != elastic2024.ID {
		t.Fatalf("2025 réel : attendu le contrat 2024 (court jusqu'au suivant), obtenu %+v", res[r.TechnoElastic])
	}

	// réel, 2027 : le 2026 est le plus récent antérieur
	res, _ = d.ResoudreContrats(2027, nil)
	if res[r.TechnoElastic].ID != elastic2026.ID {
		t.Fatalf("2027 réel : attendu le contrat 2026, obtenu %+v", res[r.TechnoElastic])
	}

	// réel, 2023 : rien d'antérieur
	res, _ = d.ResoudreContrats(2023, nil)
	if len(res) != 0 {
		t.Fatalf("2023 réel : aucun contrat attendu, obtenu %+v", res)
	}

	// scénario, 2025 : la surcharge du scénario
	res, _ = d.ResoudreContrats(2025, &sc)
	if res[r.TechnoElastic].ID != elastic2025Sc.ID {
		t.Fatalf("2025 scénario : attendu la surcharge 2025, obtenu %+v", res[r.TechnoElastic])
	}

	// scénario, 2026 : scénario avant réel — la surcharge 2025 du scénario
	// l'emporte sur le contrat réel 2026
	res, _ = d.ResoudreContrats(2026, &sc)
	if res[r.TechnoElastic].ID != elastic2025Sc.ID {
		t.Fatalf("2026 scénario : attendu la surcharge 2025 du scénario (scénario avant réel), obtenu %+v", res[r.TechnoElastic])
	}

	// scénario, 2024 : le scénario n'a rien d'antérieur, repli sur le réel
	res, _ = d.ResoudreContrats(2024, &sc)
	if res[r.TechnoElastic].ID != elastic2024.ID {
		t.Fatalf("2024 scénario : attendu le repli sur le réel 2024, obtenu %+v", res[r.TechnoElastic])
	}

	// la liste sous scénario montre réel et surcharges ensemble, réel d'abord
	// à année égale, triés par techno puis année
	liste, err := d.ListerLicenceContrats(&sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(liste) != 3 || liste[0].ID != elastic2024.ID || liste[1].ID != elastic2025Sc.ID || liste[2].ID != elastic2026.ID {
		t.Fatalf("liste sous scénario inattendue : %+v", liste)
	}
	if liste, _ := d.ListerLicenceContrats(nil); len(liste) != 2 {
		t.Fatalf("liste réelle : attendu 2 contrats, obtenu %+v", liste)
	}
}
