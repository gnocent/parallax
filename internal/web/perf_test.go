package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
)

// Charge à dix ans d'usage (revue de performance, 2026-09-12). Volumes
// visés, au-delà de l'ordre de grandeur du cadrage (~2000 serveurs) :
//
//	5 000 serveurs (dont 500 hypothétiques dans 40 scénarios), 3 vies
//	d'affectation par serveur, 2 rattachements, 40 modèles × 2 révisions
//	× 8 composants, 300 clusters, 60 000 lignes de journal, 20 000 valeurs
//	de variables, 4 000 adresses IP sur 30 VLAN.
//
// Le test mesure les pages les plus lourdes et affiche les plans de
// requête des SQL chauds : un « SCAN » sur une grosse table est ce qu'on
// cherche. Ignoré par défaut (long) : PARALLAX_PERF=1 go test ./internal/web -run TestPerformanceDixAns -v
func TestPerformanceDixAns(t *testing.T) {
	if os.Getenv("PARALLAX_PERF") == "" {
		t.Skip("PARALLAX_PERF=1 pour lancer le test de charge")
	}
	chemin := t.TempDir() + "/perf.db"
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Close() })
	debut := time.Now()
	semerDixAns(t, base)
	t.Logf("base construite en %s", time.Since(debut).Round(time.Millisecond))

	d := depot.Nouveau(base)
	hash, _ := auth.HacherMotDePasse("s3cret!")
	if _, err := d.CreerUtilisateur(depot.Utilisateur{Login: "editeur", Hash: hash, Role: depot.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	serveur := httptest.NewServer(Nouveau(d, auth.NouveauService(d)))
	t.Cleanup(serveur.Close)
	client := clientConnecte(t, serveur)

	pages := []string{
		"/serveurs", "/serveurs/tableau?filtre_statut=EN_SERVICE", "/serveurs/1", "/serveurs/sans-affectation",
		"/clusters", "/clusters/1/besoin-offre?annee=2030", "/clusters/1/dimensionnement?annee=2030",
		"/vues", "/vues/resultat?axe1=projet&axe2=techno&colonne=nb_serveurs&colonne=cpu&colonne=ram&colonne=licences_unites",
		"/vues/resultat?axe1=cluster&colonne=nb_serveurs&scenario=1",
		"/vues/resultat?axe1=cluster&colonne=nb_serveurs&date=2028-06-30",
		"/comparaison/resultat?axe1=cluster&colonne=nb_serveurs&scenario_a=0&scenario_b=1",
		"/demandes", "/demandes?scenario=1", "/adressage/anomalies", "/adressage/scenarios/1",
		"/journal", "/journal?entite=serveur", "/journal/historique/serveur/1",
		"/serveurs?export=csv", "/serveurs/export-maj", "/vues/resultat.xlsx?axe1=cluster&colonne=nb_serveurs",
		"/licences", "/modeles/1", "/variables/1", "/scenarios", "/vlans",
	}
	for _, p := range pages {
		t0 := time.Now()
		rep, err := client.Get(serveur.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		corps := corps(t, rep)
		duree := time.Since(t0)
		if rep.StatusCode != http.StatusOK {
			t.Errorf("%s : %d %s", p, rep.StatusCode, corps[:min(200, len(corps))])
		}
		t.Logf("%-95s %8s  %7d o", p, duree.Round(time.Millisecond), len(corps))
		if duree > 2*time.Second {
			t.Errorf("%s : %s, trop lent", p, duree)
		}
	}

	plans := []struct{ nom, sql string }{
		{"affectation active d'un serveur", `SELECT id FROM affectation WHERE serveur_id = 42 AND scenario_id IS NULL AND date_fin IS NULL`},
		{"affectations d'un cluster à une date", `SELECT id FROM affectation WHERE cluster_id = 7 AND scenario_id IS NULL AND date_debut <= '2030-01-01' AND (date_fin IS NULL OR date_fin >= '2030-01-01')`},
		{"rattachement ouvert", `SELECT id FROM serveur_revision WHERE serveur_id = 42 AND date_fin IS NULL`},
		{"composants d'une révision", `SELECT code FROM composant WHERE revision_id = 5`},
		{"journal par entité", `SELECT id FROM journal WHERE entite = 'serveur' AND entite_id = 42 ORDER BY id DESC`},
		{"journal par auteur", `SELECT id FROM journal WHERE utilisateur_id = 1 ORDER BY id DESC LIMIT 500`},
		{"purge du journal", `SELECT COUNT(*) FROM journal WHERE horodatage < '2024-01-01'`},
		{"serveur par hôte", `SELECT id FROM serveur WHERE scenario_id IS NULL AND hostname = 'h-1'`},
		{"serveur par demande + fiche", `SELECT id FROM serveur WHERE scenario_id IS NULL AND demande_ref = 'D-1' AND demande_serveur_ref = 'F-1'`},
		{"serveurs d'un VLAN", `SELECT id FROM serveur WHERE vlan_id = 3`},
		{"plages d'un VLAN", `SELECT id FROM plage_ip WHERE vlan_id = 3`},
		{"valeurs de variable", `SELECT id FROM variable_valeur WHERE variable_id = 3 AND annee = 2030 AND scenario_id IS NULL`},
		{"historique d'une valeur", `SELECT id FROM variable_valeur_historique WHERE valeur_id = 9`},
		{"vues d'un propriétaire", `SELECT id FROM vue WHERE proprietaire = 1 OR partagee = 1`},
		{"nœuds d'une révision", `SELECT nb_noeuds FROM modele_noeud WHERE revision_id = 5 AND techno_id = 2`},
	}
	for _, p := range plans {
		lignes, err := base.Query("EXPLAIN QUERY PLAN " + p.sql)
		if err != nil {
			t.Fatal(err)
		}
		var etapes []string
		for lignes.Next() {
			var id, parent, notused int
			var detail string
			if err := lignes.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			etapes = append(etapes, detail)
		}
		lignes.Close()
		plan := strings.Join(etapes, " | ")
		marque := "   "
		if strings.Contains(plan, "SCAN") && !strings.Contains(plan, "USING INDEX") && !strings.Contains(plan, "COVERING INDEX") {
			marque = "!! "
		}
		t.Logf("%s%-40s %s", marque, p.nom, plan)
	}
}

// semerDixAns remplit la base par SQL direct, en une transaction par table,
// avec des dates étalées de 2020 à 2030.
func semerDixAns(t *testing.T, base *sql.DB) {
	t.Helper()
	exec := func(tx *sql.Tx, q string, args ...any) {
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatalf("%s : %v", q[:min(60, len(q))], err)
		}
	}
	tx, err := base.Begin()
	if err != nil {
		t.Fatal(err)
	}
	// référentiels
	for i := 1; i <= 6; i++ {
		exec(tx, `INSERT INTO projet (id, code, libelle) VALUES (?, ?, ?)`, i, fmt.Sprintf("P%d", i), fmt.Sprintf("Projet %d", i))
	}
	for i, e := range []string{"PROD", "PREPROD", "QUAL", "DEV"} {
		exec(tx, `INSERT INTO environnement (id, code, libelle, ordre) VALUES (?, ?, ?, ?)`, i+1, e, e, i)
	}
	for i, tech := range []string{"ELASTIC", "KAFKA", "NOMAD", "LOGSTASH", "MINIO"} {
		exec(tx, `INSERT INTO techno (id, code, libelle) VALUES (?, ?, ?)`, i+1, tech, tech)
	}
	for i, tier := range []string{"HOT", "WARM", "COLD"} {
		exec(tx, `INSERT INTO tier (id, code, libelle, ordre) VALUES (?, ?, ?, ?)`, i+1, tier, tier, i)
	}
	exec(tx, `INSERT INTO usage_fonctionnel (id, code, libelle) VALUES (1, 'INGEST', 'Ingestion'), (2, 'SEARCH', 'Recherche')`)
	for i := 1; i <= 4; i++ {
		exec(tx, `INSERT INTO zone (id, code, libelle) VALUES (?, ?, ?)`, i, fmt.Sprintf("DC%d", i), fmt.Sprintf("Zone %d", i))
	}
	// 300 clusters
	for i := 1; i <= 300; i++ {
		exec(tx, `INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id, usage_fonctionnel_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			i, fmt.Sprintf("Cluster%03d", i), i%6+1, i%4+1, i%5+1, i%3+1, i%2+1)
	}
	// 40 modèles × 2 révisions × 8 composants, nœuds par techno
	rev := 0
	for m := 1; m <= 40; m++ {
		exec(tx, `INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht) VALUES (?, ?, ?, ?, ?, ?)`,
			m, fmt.Sprintf("T%d", m%4), 2020+m/4, fmt.Sprintf("T%d-%d", m%4, 2020+m/4), 20000+m*100, 4000+m*10)
		for n := 1; n <= 2; n++ {
			rev++
			exec(tx, `INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (?, ?, ?, ?)`, rev, m, n, fmt.Sprintf("%d-01-01", 2020+m%10))
			for _, c := range []struct {
				code, nature, unite string
				q, cap              float64
			}{
				{"cpu", "CPU", "CORE", 1, 64}, {"specrate", "CPU", "POINT", 1, 400}, {"ram", "RAM", "GO", 1, 1024},
				{"hdd", "DISQUE_DATA", "TO", 24, 24}, {"ssd", "DISQUE_DATA", "TO", 4, 7.68}, {"nic", "NIC", "GBPS", 2, 25},
				{"gpu", "GPU", "UNITE", 2, 1}, {"gpu_ram", "GPU", "GO", 2, 80},
			} {
				exec(tx, `INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES (?, ?, ?, ?, ?, ?)`,
					rev, c.nature, c.code, c.q, c.cap, c.unite)
			}
			for tech := 1; tech <= 5; tech++ {
				exec(tx, `INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds) VALUES (?, ?, ?)`, rev, tech, 2+tech%3)
			}
		}
	}
	// 40 scénarios, 30 VLAN avec plages
	for s := 1; s <= 40; s++ {
		exec(tx, `INSERT INTO scenario (id, nom, description, projet_id, statut, date_creation) VALUES (?, ?, ?, ?, ?, ?)`,
			s, fmt.Sprintf("Scénario %d", s), "charge", s%6+1, []string{"ACTIF", "RETENU", "ABANDONNE", "BROUILLON"}[s%4], fmt.Sprintf("%d-03-01T00:00:00Z", 2020+s%10))
	}
	for v := 1; v <= 30; v++ {
		exec(tx, `INSERT INTO vlan (id, code, projet_id, environnement_id, zone_id) VALUES (?, ?, ?, ?, ?)`, v, fmt.Sprintf("VL-%02d", v), v%6+1, v%4+1, v%4+1)
		exec(tx, `INSERT INTO plage_ip (vlan_id, ip_debut, ip_fin) VALUES (?, ?, ?)`, v, fmt.Sprintf("10.%d.0.10", v), fmt.Sprintf("10.%d.3.250", v))
	}
	// 5 000 serveurs : 4 500 réels (2 vies + 1 en cours), 500 hypothétiques
	for s := 1; s <= 5000; s++ {
		var scenario any
		statut := []string{"EN_SERVICE", "EN_SERVICE", "EN_SERVICE", "COMMANDE", "DECOMMISSIONNE"}[s%5]
		if s > 4500 {
			scenario, statut = (s%40)+1, "HYPOTHESE"
		}
		var ip any
		if s%5 != 4 && s <= 4000 {
			ip = fmt.Sprintf("10.%d.%d.%d", s%30+1, (s/250)%4, 10+s%240)
		}
		exec(tx, `INSERT INTO serveur (id, physical_name, hostname, serial_number, zone_id, ip, vlan_id, statut, scenario_id, date_entree, demande_ref, demande_serveur_ref, typologie)
		          VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'PAD')`,
			s, fmt.Sprintf("PHY-%05d", s), fmt.Sprintf("h-%d", s), fmt.Sprintf("SN%06d", s), s%4+1, ip, s%30+1, statut, scenario,
			fmt.Sprintf("%d-%02d-01", 2020+s%6, s%12+1), fmt.Sprintf("D-%d", s/20), fmt.Sprintf("F-%d", s))
		// rattachements : révision d'origine puis mise à niveau ; dates
		// cohérentes par année de base y (entrée), toujours fin ≥ début.
		y := 2020 + s%6
		r1 := (s % 80) + 1
		exec(tx, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut, date_fin) VALUES (?, ?, ?, ?)`, s, r1, fmt.Sprintf("%d-01-01", y), fmt.Sprintf("%d-12-31", y+1))
		exec(tx, `INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, ?, ?)`, s, r1%80+1, fmt.Sprintf("%d-01-01", y+2))
		// affectations : deux vies closes puis une ouverte (réel), une seule pour un hypothétique
		if s > 4500 {
			exec(tx, `INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id) VALUES (?, ?, ?, ?)`, s, s%300+1, "2030-01-01", scenario)
			continue
		}
		exec(tx, `INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin) VALUES (?, ?, ?, ?)`, s, s%300+1, fmt.Sprintf("%d-01-01", y), fmt.Sprintf("%d-06-30", y+1))
		exec(tx, `INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin) VALUES (?, ?, ?, ?)`, s, (s+7)%300+1, fmt.Sprintf("%d-07-01", y+1), fmt.Sprintf("%d-12-31", y+2))
		exec(tx, `INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (?, ?, ?)`, s, (s+13)%300+1, fmt.Sprintf("%d-01-01", y+3))
	}
	// 20 000 valeurs de variables, 10 variables sur 10 ans et 200 portées
	exec(tx, `INSERT INTO metrique (id, code, libelle, unite) VALUES (1, 'DISQUE_UTILE_TO', 'Disque utile', 'TO'), (2, 'RAM_GO', 'RAM', 'GO')`)
	for v := 1; v <= 10; v++ {
		exec(tx, `INSERT INTO variable (id, code, libelle, defaut) VALUES (?, ?, ?, 1)`, v, fmt.Sprintf("var%d", v), fmt.Sprintf("Variable %d", v))
		for a := 2020; a <= 2029; a++ {
			for c := 1; c <= 200; c++ {
				exec(tx, `INSERT INTO variable_valeur (variable_id, annee, cluster_id, valeur, modifie_le) VALUES (?, ?, ?, ?, ?)`, v, a, c, float64(v*a%97), "2026-01-01T00:00:00Z")
			}
		}
	}
	exec(tx, `INSERT INTO regle (nom, metrique_id, expression, niveau_evaluation, composant_offre, techno_id, actif, date_creation) VALUES ('Disque', 1, 'var1 * var2', 'PERIMETRE', 'hdd', 1, 1, '2026-01-01T00:00:00Z')`)
	exec(tx, `INSERT INTO regle (nom, metrique_id, expression, niveau_evaluation, composant_offre, techno_id, actif, date_creation) VALUES ('RAM', 2, 'ceil(ram_machine / 64)', 'PAR_SERVEUR', 'ram', 1, 1, '2026-01-01T00:00:00Z')`)
	exec(tx, `INSERT INTO licence_contrat (techno_id, annee, mecanisme, niveau, ram_max_go, cout_unitaire_ht) VALUES (1, 2026, 'RAM', 'GLOBAL', 512, 1000)`)
	// 60 000 lignes de journal, 4 ans, sur les serveurs
	for j := 1; j <= 60000; j++ {
		exec(tx, `INSERT INTO journal (entite, entite_id, action, utilisateur_id, horodatage, avant, apres) VALUES ('serveur', ?, 'MODIFICATION', NULL, ?, '{"a":1}', '{"a":2}')`,
			j%5000+1, fmt.Sprintf("%d-%02d-%02dT10:00:00Z", 2023+j%4, j%12+1, j%28+1))
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
