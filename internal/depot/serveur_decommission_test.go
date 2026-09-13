package depot

import "testing"

// TestDecommissionnementSortDuParc (revue 2026-09-12) : passer un serveur en
// DECOMMISSIONNE pose sa date de sortie, vide son adresse, et clôt son
// affectation réelle et son rattachement ouverts — il ne compte plus dans
// l'offre ni dans les vues. Le retour en service efface la date de sortie
// sans rouvrir les périodes.
func TestDecommissionnementSortDuParc(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	cluster := clusterDeTest(t, d.base, r, "ElasticHot")
	m, err := d.CreerModele(Modele{Type: "STD", Annee: 2024, Code: "STD-2024"})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := d.CreerRevision(Revision{ModeleID: m.ID, DateEffet: "2024-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	nom, ip := "PHY-01", "10.0.0.5"
	s, err := d.CreerServeur(Serveur{PhysicalName: &nom, IP: &ip, Statut: StatutEnService, DateEntree: ptrS("2024-01-01")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.RattacherRevision(s.ID, rev.ID, "2024-01-01", nil); err != nil {
		t.Fatal(err)
	}
	if err := d.Affecter(s.ID, cluster, "2024-02-01", nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := d.ChangerStatut(s.ID, StatutDecommissionne); err != nil {
		t.Fatal(err)
	}
	apres, _ := d.LireServeur(s.ID)
	if apres.DateSortie == nil || apres.IP != nil || apres.Statut != StatutDecommissionne {
		t.Fatalf("date de sortie posée et adresse vidée attendues : %+v", apres)
	}
	vies, _ := d.ListerAffectationsServeur(s.ID)
	if len(vies) != 1 || vies[0].DateFin == nil || *vies[0].DateFin != *apres.DateSortie {
		t.Fatalf("affectation close à la date de sortie attendue : %+v", vies)
	}
	ratts, _ := d.ListerRattachements(s.ID)
	if len(ratts) != 1 || ratts[0].DateFin == nil {
		t.Fatalf("rattachement clos attendu : %+v", ratts)
	}
	if actives, _ := d.ListerAffectationsCluster(cluster, nil, Horodatage()[:10]); len(actives) != 0 {
		// la date de fin est aujourd'hui : à la date du jour la période est
		// encore incluse (bornes fermées), on vérifie donc à demain.
		_ = actives
	}
	if actives, _ := d.ListerAffectationsCluster(cluster, nil, "2999-01-01"); len(actives) != 0 {
		t.Fatalf("le serveur décommissionné ne doit plus compter dans le cluster : %+v", actives)
	}

	// journal : serveur, affectation et rattachement clos, chacun tracé
	for _, entite := range []string{"serveur", "affectation", "serveur_revision"} {
		entrees, _ := d.ListerJournal(FiltreJournal{Entite: entite, Action: ActionModification})
		if len(entrees) == 0 {
			t.Fatalf("la clôture de %s doit être journalisée", entite)
		}
	}

	// retour en service : la date de sortie s'efface, rien ne se rouvre
	if err := d.ChangerStatut(s.ID, StatutEnService); err != nil {
		t.Fatal(err)
	}
	retour, _ := d.LireServeur(s.ID)
	if retour.DateSortie != nil {
		t.Fatalf("la date de sortie doit être effacée au retour en service : %+v", retour)
	}
	if vies, _ := d.ListerAffectationsServeur(s.ID); vies[0].DateFin == nil {
		t.Fatal("le retour en service ne rouvre pas l'affectation close")
	}

	// une affectation qui commence après la sortie est close à son début, pas avant
	nom2 := "PHY-02"
	s2, _ := d.CreerServeur(Serveur{PhysicalName: &nom2, Statut: StatutCommande, DateEntree: ptrS("2024-01-01")})
	if err := d.Affecter(s2.ID, cluster, "2999-06-01", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := d.ChangerStatut(s2.ID, StatutDecommissionne); err != nil {
		t.Fatal(err)
	}
	if vies, _ := d.ListerAffectationsServeur(s2.ID); vies[0].DateFin == nil || *vies[0].DateFin != "2999-06-01" {
		t.Fatalf("clôture au plus tôt au début de la période attendue : %+v", vies)
	}
}

// TestNomsDeServeurUniquesParSeau : l'index d'unicité (migration 0006)
// refuse deux serveurs réels de même nom physique ou de même hôte, mais
// laisse deux scénarios porter le même nom hypothétique.
func TestNomsDeServeurUniquesParSeau(t *testing.T) {
	d := depotDeTest(t)
	r := semerRefs(t, d.base)
	nom := "PHY-01"
	if _, err := d.CreerServeur(Serveur{PhysicalName: &nom, Statut: StatutCommande, DateEntree: ptrS("2024-01-01")}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerServeur(Serveur{PhysicalName: &nom, Statut: StatutCommande, DateEntree: ptrS("2024-01-01")}); err == nil {
		t.Fatal("deux serveurs réels de même nom physique doivent être refusés")
	}
	hote := "esh01"
	if _, err := d.CreerServeur(Serveur{Hostname: &hote, Statut: StatutCommande, DateEntree: ptrS("2024-01-01")}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerServeur(Serveur{Hostname: &hote, Statut: StatutCommande, DateEntree: ptrS("2024-01-01")}); err == nil {
		t.Fatal("deux serveurs réels de même hôte doivent être refusés")
	}
	s1, err := d.CreerScenario(Scenario{Nom: "A", Description: "a", ProjetID: &r.Projet})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := d.CreerScenario(Scenario{Nom: "B", Description: "b", ProjetID: &r.Projet})
	if err != nil {
		t.Fatal(err)
	}
	hyp := "ELK-01"
	if _, err := d.CreerServeur(Serveur{PhysicalName: &hyp, Statut: StatutHypothese, ScenarioID: &s1.ID, DateEntree: ptrS("2026-01-01")}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerServeur(Serveur{PhysicalName: &hyp, Statut: StatutHypothese, ScenarioID: &s2.ID, DateEntree: ptrS("2026-01-01")}); err != nil {
		t.Fatalf("deux scénarios peuvent porter le même nom hypothétique : %v", err)
	}
}
