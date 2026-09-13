package depot

import (
	"errors"
	"testing"
)

// TestAppliquerMajServeursAtomique : un plan multi-serveurs s'applique en
// une transaction (attributs, statut, rattachement, réaffectation), chaque
// geste est journalisé, et un plan dont une ligne échoue n'écrit rien.
func TestAppliquerMajServeursAtomique(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	clusterA := clusterDeTest(t, d.base, r, "ElasticHot")
	clusterB := clusterDeTest(t, d.base, r, "ElasticCold")

	m, err := d.CreerModele(Modele{Type: "STD", Annee: 2024, Code: "STD-2024"})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := d.CreerRevision(Revision{ModeleID: m.ID, Libelle: ptrS("origine"), DateEffet: "2024-01-01"})
	if err != nil {
		t.Fatal(err)
	}

	nom := "PHY-01"
	s1, err := d.CreerServeur(Serveur{PhysicalName: &nom, Statut: StatutCommande, DateEntree: ptrS("2024-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	nom2 := "PHY-02"
	s2, err := d.CreerServeur(Serveur{PhysicalName: &nom2, Statut: StatutEnService, DateEntree: ptrS("2024-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(s2.ID, clusterA, "2024-02-01", nil, nil); err != nil {
		t.Fatal(err)
	}

	hote := "esh01"
	attributs := s1
	attributs.Hostname = &hote
	statut := StatutEnService
	plans := []MajServeur{
		{ServeurID: s1.ID, Attributs: &attributs, Statut: &statut,
			Rattachement: &MajRattachement{RevisionID: rev.ID, Date: "2024-03-01"},
			Affectation:  &MajAffectation{ClusterID: clusterA, Date: "2024-03-01"}},
		{ServeurID: s2.ID, Affectation: &MajAffectation{ClusterID: clusterB, Date: "2024-06-01", Reaffecter: true}},
		{ServeurID: s2.ID}, // plan vide : ne compte pas
	}
	n, err := d.AppliquerMajServeurs(plans)
	if err != nil || n != 2 {
		t.Fatalf("attendu 2 serveurs modifiés, obtenu %d (%v)", n, err)
	}

	maj1, _ := d.LireServeur(s1.ID)
	if maj1.Hostname == nil || *maj1.Hostname != "esh01" || maj1.Statut != StatutEnService {
		t.Fatalf("attributs et statut attendus : %+v", maj1)
	}
	ratt, _ := d.ListerRattachements(s1.ID)
	if len(ratt) != 1 || ratt[0].RevisionID != rev.ID {
		t.Fatalf("rattachement attendu : %+v", ratt)
	}
	vies, _ := d.ListerAffectationsServeur(s2.ID)
	if len(vies) != 2 || vies[0].DateFin == nil || *vies[0].DateFin != "2024-05-31" || vies[1].ClusterID != clusterB {
		t.Fatalf("réaffectation attendue (clôture la veille, ouverture sur B) : %+v", vies)
	}
	journal, _ := d.ListerJournal(FiltreJournal{Entite: "affectation"})
	if len(journal) < 3 { // création s2, clôture, ouverture B, ouverture s1
		t.Fatalf("les gestes d'affectation doivent être journalisés : %d entrées", len(journal))
	}

	// une ligne en échec annule tout : PHY-02 réaffecté à une date
	// antérieure au début courant (chevauchement) — PHY-01 ne doit pas
	// changer de hostname.
	autre := "esh01-bis"
	attributs2 := maj1
	attributs2.Hostname = &autre
	_, err = d.AppliquerMajServeurs([]MajServeur{
		{ServeurID: s1.ID, Attributs: &attributs2},
		{ServeurID: s2.ID, Affectation: &MajAffectation{ClusterID: clusterA, Date: "2024-01-15", Reaffecter: true}},
	})
	if err == nil || !errors.Is(err, ErrValidation) && !errors.Is(err, ErrChevauchement) {
		t.Fatalf("attendu un refus du plan, obtenu %v", err)
	}
	apres, _ := d.LireServeur(s1.ID)
	if *apres.Hostname != "esh01" {
		t.Fatalf("un plan refusé ne doit rien écrire, hostname = %s", *apres.Hostname)
	}

	// résolution par clé
	if ids, _ := d.ServeursParCle("hostname", "esh01"); len(ids) != 1 || ids[0] != s1.ID {
		t.Fatalf("résolution par hostname : %v", ids)
	}
	if ids, _ := d.ServeursParCle("physical_name", "PHY-02"); len(ids) != 1 || ids[0] != s2.ID {
		t.Fatalf("résolution par nom physique : %v", ids)
	}
	if _, err := d.ServeursParCle("commentaire", "x"); !errors.Is(err, ErrValidation) {
		t.Fatal("une colonne hors clé doit être refusée")
	}
}
