package vues

import (
	"database/sql"
	"path/filepath"
	"testing"

	"parallax/internal/db"
)

// baseDeTest sème un jeu de données minimal mais représentatif : deux
// clusters Elastic (HOT et COLD) sur le même projet/environnement, un
// cluster Kafka sans tier, quatre serveurs affectés avec des révisions et
// des composants différents. De quoi exercer le regroupement multi-axes, les
// filtres et les agrégats.
func baseDeTest(t *testing.T) *sql.DB {
	t.Helper()
	base, err := db.Ouvrir(filepath.Join(t.TempDir(), "vues-test.db"))
	if err != nil {
		t.Fatalf("ouverture de la base de test : %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch'), (2, 'KAFKA', 'Kafka');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10), (2, 'COLD', 'Cold', 30);
	INSERT INTO zone (id, code, libelle) VALUES (1, 'DC1', 'Zone 1');

	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1),
		(2, 'ElasticCold1', 1, 1, 1, 2),
		(3, 'Kafka', 1, 1, 2, NULL);

	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht) VALUES
		(1, 'STD', 2020, 'STD-2020', 18000, 4200);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES
		(1, 'CPU', 'cpu', 1, 40, 'CORE'),
		(1, 'RAM', 'ram', 1, 768, 'GO'),
		(1, 'DISQUE_DATA', 'ssd', 12, 8, 'TO');
	INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds) VALUES (1, 1, 4), (1, 2, 4);

	INSERT INTO serveur (id, physical_name, zone_id, statut, date_entree) VALUES
		(1, 'PHY001', 1, 'EN_SERVICE', '2020-01-01'),
		(2, 'PHY002', 1, 'EN_SERVICE', '2020-01-01'),
		(3, 'PHY003', 1, 'EN_SERVICE', '2020-01-01'),
		(4, 'PHY004', 1, 'EN_SERVICE', '2020-01-01');

	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES
		(1, 1, '2020-01-01'), (2, 1, '2020-01-01'), (3, 1, '2020-01-01');
	-- PHY004 n'a pas de rattachement de révision : ses mesures doivent être 0

	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES
		(1, 1, '2020-01-01'),
		(2, 2, '2020-01-01'),
		(3, 3, '2020-01-01'),
		(4, 1, '2020-01-01');
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}
	return base
}

func TestChargerParcCourant(t *testing.T) {
	base := baseDeTest(t)
	lignes, err := ChargerParcCourant(base, nil, "2026-01-01")
	if err != nil {
		t.Fatalf("chargement : %v", err)
	}
	if len(lignes) != 4 {
		t.Fatalf("attendu 4 lignes (une par serveur affecté), obtenu %d", len(lignes))
	}

	var phy001 *Ligne
	for i := range lignes {
		if lignes[i].ServeurID == 1 {
			phy001 = &lignes[i]
		}
	}
	if phy001 == nil {
		t.Fatal("PHY001 absent du parc courant")
	}
	if phy001.ClusterNom != "ElasticHot" || phy001.TierCode != "HOT" || phy001.ProjetCode != "LOGS" {
		t.Fatalf("dimensions inattendues pour PHY001 : %+v", phy001)
	}
	if phy001.Capacite("cpu") != 40 || phy001.Capacite("ram") != 768 || phy001.Capacite("ssd") != 96 || phy001.Quantite("ssd") != 12 {
		t.Fatalf("mesures inattendues pour PHY001 (40 cœurs, 768 Go, 12x8 To) : %+v", phy001)
	}
	if phy001.NbNoeuds != 4 {
		t.Fatalf("nb_noeuds attendu 4 (techno Elastic du cluster), obtenu %v", phy001.NbNoeuds)
	}
	if phy001.TechnoID != 1 || phy001.ClusterID != 1 {
		t.Fatalf("identifiants de techno et de cluster attendus (1, 1) pour PHY001, obtenu (%d, %d)", phy001.TechnoID, phy001.ClusterID)
	}

	var phy004 *Ligne
	for i := range lignes {
		if lignes[i].ServeurID == 4 {
			phy004 = &lignes[i]
		}
	}
	if phy004 == nil {
		t.Fatal("PHY004 absent")
	}
	if phy004.Capacite("cpu") != 0 || phy004.ModeleCode != "" {
		t.Fatalf("serveur sans révision rattachée : mesures et modèle doivent être vides/nuls : %+v", phy004)
	}
}

// TestChargerParcCourantSousScenario couvre les trois surcharges possibles
// (déplacement, retrait, ajout hypothétique), avec la même règle qu'à la
// vérification (voir depot.AffectationsResolues et modele-donnees.md §6) :
// un serveur touché par le scénario n'apparaît plus sous sa forme réelle.
func TestChargerParcCourantSousScenario(t *testing.T) {
	base := baseDeTest(t)
	script := `
	INSERT INTO scenario (id, nom, description, statut, date_creation)
		VALUES (1, 'Hypothèse', 'test', 'ACTIF', '2026-01-01');

	-- PHY002 (réel : ElasticCold1) bascule sur Kafka dans le scénario.
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id)
		VALUES (2, 3, '2026-02-01', 1);

	-- PHY003 (réel : Kafka) est retiré dans le scénario, sans réaffectation :
	-- une ligne ouverte puis refermée dans le seau du scénario.
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin, scenario_id)
		VALUES (3, 3, '2026-02-01', '2026-03-01', 1);

	-- PHY005 est un ajout pur, hypothétique, sur ElasticHot.
	INSERT INTO serveur (id, physical_name, statut, scenario_id, date_entree)
		VALUES (5, 'PHY005', 'HYPOTHESE', 1, '2026-02-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id)
		VALUES (5, 1, '2026-02-01', 1);
	`
	if _, err := base.Exec(script); err != nil {
		t.Fatalf("fixture scénario : %v", err)
	}

	sc := int64(1)
	lignes, err := ChargerParcCourant(base, &sc, "2026-06-01")
	if err != nil {
		t.Fatalf("chargement sous scénario : %v", err)
	}

	parServeur := map[int64]Ligne{}
	for _, l := range lignes {
		parServeur[l.ServeurID] = l
	}

	// PHY001 et PHY004 : intacts, toujours sur ElasticHot.
	if parServeur[1].ClusterNom != "ElasticHot" {
		t.Fatalf("PHY001 doit rester sur ElasticHot : %+v", parServeur[1])
	}
	if parServeur[4].ClusterNom != "ElasticHot" {
		t.Fatalf("PHY004 doit rester sur ElasticHot : %+v", parServeur[4])
	}
	// PHY002 : déplacé sur Kafka, plus sur ElasticCold1.
	if parServeur[2].ClusterNom != "Kafka" {
		t.Fatalf("PHY002 doit apparaître sur Kafka sous le scénario : %+v", parServeur[2])
	}
	// PHY003 : retiré, ne doit plus apparaître du tout.
	if _, present := parServeur[3]; present {
		t.Fatalf("PHY003 doit être absent (retiré par le scénario) : %+v", parServeur[3])
	}
	// PHY005 : ajout hypothétique, sur ElasticHot.
	if parServeur[5].ClusterNom != "ElasticHot" {
		t.Fatalf("PHY005 (ajout hypothétique) doit apparaître sur ElasticHot : %+v", parServeur[5])
	}
	if len(lignes) != 4 {
		t.Fatalf("attendu 4 lignes sous scénario (1,4,2,5 ; 3 retiré) : %+v", lignes)
	}

	// le réel reste inchangé : toujours 4 lignes, PHY003 toujours sur Kafka.
	reel, err := ChargerParcCourant(base, nil, "2026-01-01")
	if err != nil {
		t.Fatalf("chargement réel : %v", err)
	}
	if len(reel) != 4 {
		t.Fatalf("le réel ne doit pas être affecté par le scénario : %+v", reel)
	}
}

func TestFiltrer(t *testing.T) {
	lignes := []Ligne{
		{ServeurID: 1, TierCode: "HOT"},
		{ServeurID: 2, TierCode: "COLD"},
		{ServeurID: 3, TierCode: ""},
	}
	f := Filtre{DimTier: {"HOT", "COLD"}}
	obtenu := Filtrer(lignes, f)
	if len(obtenu) != 2 {
		t.Fatalf("attendu 2 lignes filtrées, obtenu %d : %+v", len(obtenu), obtenu)
	}

	// filtre vide = pas de restriction
	if len(Filtrer(lignes, Filtre{})) != 3 {
		t.Fatal("un filtre vide ne doit rien exclure")
	}
	if len(Filtrer(lignes, Filtre{DimTier: nil})) != 3 {
		t.Fatal("une dimension à ensemble vide ne doit rien exclure")
	}
}

func TestValeursDisponibles(t *testing.T) {
	base := baseDeTest(t)
	lignes, _ := ChargerParcCourant(base, nil, "2026-01-01")
	dispo := ValeursDisponibles(lignes)

	if got := dispo[DimTier]; len(got) != 2 || got[0] != "COLD" || got[1] != "HOT" {
		t.Fatalf("valeurs de tier attendues [COLD HOT] triées, obtenu %v", got)
	}
	if got := dispo[DimCluster]; len(got) != 3 {
		t.Fatalf("attendu 3 clusters distincts, obtenu %v", got)
	}
}

func TestRegrouperParTierAvecAgregats(t *testing.T) {
	base := baseDeTest(t)
	lignes, _ := ChargerParcCourant(base, nil, "2026-01-01")

	groupes := Regrouper(lignes, []Dimension{DimTechno, DimTier},
		[]Colonne{ColNbServeurs, "cpu", "ssd"})

	// attendu : (ELASTIC,HOT)=2 serveurs (PHY001+PHY004, PHY004 à 0),
	// (ELASTIC,COLD)=1 (PHY002), (KAFKA,"")=1 (PHY003)
	if len(groupes) != 3 {
		t.Fatalf("attendu 3 groupes, obtenu %d : %+v", len(groupes), groupes)
	}

	var hotElastic *Groupe
	for i := range groupes {
		if groupes[i].Cles[0] == "ELASTIC" && groupes[i].Cles[1] == "HOT" {
			hotElastic = &groupes[i]
		}
	}
	if hotElastic == nil {
		t.Fatalf("groupe ELASTIC/HOT introuvable parmi %+v", groupes)
	}
	if hotElastic.Valeurs[ColNbServeurs] != 2 {
		t.Fatalf("ELASTIC/HOT : attendu 2 serveurs, obtenu %v", hotElastic.Valeurs[ColNbServeurs])
	}
	if hotElastic.Valeurs["cpu"] != 40 { // seul PHY001 a des composants
		t.Fatalf("ELASTIC/HOT : attendu 40 cœurs cumulés, obtenu %v", hotElastic.Valeurs["cpu"])
	}
}

func TestRegrouperSansAxeDonneUnTotal(t *testing.T) {
	base := baseDeTest(t)
	lignes, _ := ChargerParcCourant(base, nil, "2026-01-01")
	groupes := Regrouper(lignes, nil, []Colonne{ColNbServeurs})
	if len(groupes) != 1 || groupes[0].Valeurs[ColNbServeurs] != 4 {
		t.Fatalf("sans axe : attendu un seul groupe de 4 serveurs, obtenu %+v", groupes)
	}
}
