package web

import (
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// TestExportUniverselProjets vérifie la convention d'export sur l'écran de
// référence (projets) : les deux liens sont posés dans la page avec les
// paramètres courants, et chaque lien renvoie un fichier au bon type.
func TestExportUniverselProjets(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	if _, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Plateforme de logs"}); err != nil {
		t.Fatal(err)
	}

	rep, err := client.Get(serveur.URL + "/projets?inclure_inactifs=1")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	for _, lien := range []string{`href="/projets?export=csv&amp;inclure_inactifs=1"`, `href="/projets?export=xlsx&amp;inclure_inactifs=1"`} {
		if !strings.Contains(page, lien) {
			t.Fatalf("lien d'export attendu %s dans la page : %s", lien, page)
		}
	}

	repCSV, err := client.Get(serveur.URL + "/projets?export=csv")
	if err != nil {
		t.Fatal(err)
	}
	if ct := repCSV.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("export csv : type attendu text/csv, obtenu %q", ct)
	}
	if cd := repCSV.Header.Get("Content-Disposition"); !strings.Contains(cd, "projets-") || !strings.HasSuffix(cd, `.csv"`) {
		t.Fatalf("nom de fichier csv inattendu : %q", cd)
	}
	csv := corps(t, repCSV)
	if !strings.HasPrefix(csv, "Code;Libellé;Actif\n") || !strings.Contains(csv, "LOGS;Plateforme de logs;oui") {
		t.Fatalf("contenu csv inattendu : %s", csv)
	}

	repXLSX, err := client.Get(serveur.URL + "/projets?export=xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if ct := repXLSX.Header.Get("Content-Type"); !strings.Contains(ct, "spreadsheetml") {
		t.Fatalf("export xlsx : type inattendu %q", ct)
	}
	if repXLSX.StatusCode != http.StatusOK || !strings.HasPrefix(corps(t, repXLSX), "PK") {
		t.Fatal("export xlsx : archive zip attendue")
	}
}

// TestNomFichierExport : le titre devient un nom de fichier sûr.
func TestNomFichierExport(t *testing.T) {
	nom := nomFichierExport("Besoin / offre — ElasticCold1", "csv")
	if !strings.HasPrefix(nom, "besoin-offre-elasticcold1-") || !strings.HasSuffix(nom, ".csv") {
		t.Fatalf("nom inattendu : %s", nom)
	}
	if nom := nomFichierExport("", "xlsx"); !strings.HasPrefix(nom, "export-") {
		t.Fatalf("titre vide : attendu export-…, obtenu %s", nom)
	}
}

// gabaritsSansExport liste les gabarits qui contiennent un <table> sans
// être un écran de liste exportable : fragments de lignes, tableaux de
// saisie, ou tableaux dont l'export est porté par un autre gabarit.
//
// Ajouter une entrée ici est une décision, pas une commodité : la règle
// (backlog v3.0) est qu'aucun tableau affiché ne reste sans ses boutons.
var gabaritsSansExport = map[string]string{
	"templates/import/modeles.html":        "tableau de documentation du format, pas de données",
	"templates/import/maj.html":            "aperçu transitoire d'une simulation (diff à confirmer), pas une liste persistante",
	"templates/parametres/sauvegarde.html": "état du disque à l'instant présent, pas une donnée métier à archiver",
	// la synthèse et l'aperçu du lot, dans ce même fichier, ont leurs boutons ;
	// la table de choix du lot (un <select> par cluster) est une saisie
	"templates/scenarios/synthese.html": "table de choix du lot : une saisie, pas une liste ; la synthèse et l'aperçu s'exportent",
}

// gabaritsAConvertir : écrans antérieurs au socle d'export, à convertir un
// par un (essaim v3.0). Chaque conversion retire son entrée ; la liste doit
// finir vide — un écran qui y reste est une dette visible, pas une exception.
// Vide depuis la conversion de tous les écrans : un nouveau gabarit avec un
// <table> doit poser ses boutons d'emblée, ou se justifier dans
// gabaritsSansExport.
var gabaritsAConvertir = map[string]bool{}

// TestExportUniverselEcrans vérifie, écran par écran, que chaque tableau
// converti répond à ?export=csv par un text/csv dont la première ligne est
// celle du tableau affiché (et contient une ligne de données attendue), et
// à ?export=xlsx par une archive. Une seule fixture SQL, reprise de
// handlers_dimensionnement_inverse_test.go : un cluster ElasticHot avec un
// serveur installé (STD-2020, 96 To), un candidat DENSE-2027, une règle
// « Disque HOT » (35 To en 2027), un scénario et une contrainte.
func TestExportUniverselEcrans(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)

	script := `
	INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
	INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
	INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
	INSERT INTO tier (id, code, libelle, ordre) VALUES (1, 'HOT', 'Hot', 10);
	INSERT INTO usage_fonctionnel (id, code, libelle) VALUES (1, 'LOGMGMT', 'Log management');
	INSERT INTO zone (id, code, libelle, site) VALUES (1, 'DC1', 'Zone 1', 'Site A');
	INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id) VALUES
		(1, 'ElasticHot', 1, 1, 1, 1);

	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (1, 'STD', 2020, 'STD-2020', 18000, 4200);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (1, 1, 1, '2020-01-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (1, 'DISQUE_DATA', 'ssd', 12, 8, 'TO');
	INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds) VALUES (1, 1, 3);
	INSERT INTO modele (id, type, annee, code, prix_fournisseur_ht, cout_annuel_ht)
		VALUES (2, 'DENSE', 2027, 'DENSE-2027', 42000, 9800);
	INSERT INTO revision (id, modele_id, numero, date_effet) VALUES (2, 2, 1, '2027-01-01'), (3, 2, 2, '2027-03-01');
	INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
		VALUES (2, 'DISQUE_DATA', 'ssd', 12, 24, 'TO'), (3, 'DISQUE_DATA', 'ssd', 24, 24, 'TO');

	INSERT INTO serveur (id, physical_name, statut, date_entree, zone_id) VALUES (1, 'PHY001', 'EN_SERVICE', '2020-01-01', 1);
	INSERT INTO serveur (id, physical_name, hostname, statut) VALUES (2, 'PHY002', 'libre01', 'COMMANDE');
	INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (1, 1, '2020-01-01');
	INSERT INTO affectation (serveur_id, cluster_id, date_debut) VALUES (1, 1, '2020-01-01');

	INSERT INTO metrique (id, code, libelle, unite) VALUES (1, 'DISQUE_UTILE_TO', 'Disque utile', 'TO');
	INSERT INTO variable (id, code, libelle, unite, defaut) VALUES (1, 'debit', 'Débit', 'To/j', NULL), (2, 'retention', 'Rétention', 'j', 7);
	INSERT INTO variable_valeur (id, variable_id, annee, techno_id, valeur, modifie_le)
		VALUES (1, 1, 2027, 1, 5, '2027-01-01');
	INSERT INTO regle (nom, metrique_id, expression, niveau_evaluation, composant_offre, techno_id, tier_id, date_creation)
		VALUES ('Disque HOT', 1, 'debit * retention', 'PERIMETRE', 'ssd', 1, 1, '2027-01-01');

	INSERT INTO scenario (id, nom, description, statut, date_creation)
		VALUES (1, 'Renfort HOT', 'test', 'ACTIF', '2026-01-01');
	INSERT INTO contrainte (cluster_id, scenario_id, annee, type, valeur)
		VALUES (1, NULL, 2027, 'MIN_TOTAL', 3);
	`
	if _, err := d.Base().Exec(script); err != nil {
		t.Fatalf("fixture : %v", err)
	}
	// /utilisateurs est réservé aux ADMIN : promotion du compte de test.
	us, err := d.ListerUtilisateurs(false)
	if err != nil {
		t.Fatal(err)
	}
	us[0].Role = depot.RoleAdmin
	if err := d.ModifierUtilisateur(us[0]); err != nil {
		t.Fatal(err)
	}

	cas := []struct {
		route    string // avec ses paramètres (filtres, scénario, tableau nommé)
		entete   string // première ligne CSV attendue
		contient string // une ligne de données attendue
	}{
		{"/environnements", "Code;Libellé;Ordre", "PROD;Production;10"},
		{"/technos", "Code;Libellé;Actif", "ELASTIC;Elasticsearch;oui"},
		{"/tiers", "Code;Libellé;Ordre", "HOT;Hot;10"},
		{"/usages", "Code;Libellé", "LOGMGMT;Log management"},
		{"/zones", "Code;Libellé;Site", "DC1;Zone 1;Site A"},
		{"/utilisateurs", "Identifiant;Nom;Rôle;Actif;Origine", "editeur;;ADMIN;oui;LOCAL"},
		{"/metriques", "Code;Libellé;Unité", "DISQUE_UTILE_TO;Disque utile;TO"},
		{"/variables", "Code;Libellé;Unité;Défaut;Commentaire", "retention;Rétention;j;7;"},
		{"/variables/1", "Année;Projet;Environnement;Techno;Tier;Cluster;Valeur;Commentaire;Seau", "2027;;;Elasticsearch;;;5;;réel"},
		{"/variables/1/valeurs/1/historique", "Horodatage;Ancienne valeur;Nouvelle valeur", ""},
		{"/regles", "Nom;Métrique;Expression;Niveau;Filtre;Actif", "Disque HOT;Disque utile;debit * retention;Périmètre;Elasticsearch / Hot;oui"},
		{"/scenarios", "Nom;Projet;Description;Statut;Créé le", "Renfort HOT;;test;ACTIF;2026-01-01"},
		{"/modeles", "Code;Type;Année;Financement;Actif", "STD-2020;STD;2020;;oui"},
		{"/modeles/1?tableau=composants", "Révision;Nature;Code;Quantité;Capacité unitaire;Unité;Commentaire", "1;DISQUE_DATA;ssd;12;8;TO;"},
		{"/modeles/1?tableau=noeuds&revision=1", "Révision;Techno;Code techno;Nombre de nœuds", "1;Elasticsearch;ELASTIC;3"},
		{"/clusters?filtre_techno_id=1", "Nom;Projet;Environnement;Techno;Tier;Usage;Actif", "ElasticHot;Log Management;Production;Elasticsearch;Hot;;oui"},
		{"/serveurs?filtre_statut=EN_SERVICE", "Nom physique;Hostname;Zone;Statut;Scénario", "PHY001;;Zone 1;EN_SERVICE;non"},
		{"/serveurs/1?tableau=rattachements", "Modèle;Révision;Libellé;Début;Fin", "STD-2020;1;;2020-01-01;"},
		{"/serveurs/1?tableau=affectations", "Cluster;Début;Fin;Seau", "ElasticHot;2020-01-01;;réel"},
		{"/serveurs/sans-affectation", "Serveur;Hostname;Zone;Fin de lease", "PHY002;libre01;;"},
		{"/clusters/1/besoin-offre?annee=2027&tableau=besoin_offre", "Règle;Métrique;Composant;Besoin;Offre;Écart;Limitante", "Disque HOT;DISQUE_UTILE_TO;ssd;35;96;61;oui"},
		{"/clusters/1/besoin-offre?annee=2027&scenario=1&tableau=delta", "Mouvement;Serveur;Détail", ""},
		{"/clusters/1/besoin-offre?annee=2027&tableau=contraintes", "Type;Valeur / portée;Commentaire;Seau", "MIN_TOTAL;3;;réel"},
		{"/clusters/1/besoin-offre?annee=2027&scenario=1&tableau=contraintes_effectives", "Type;Valeur", "MIN_TOTAL;3"},
		{"/clusters/1/dimensionnement?annee=2027&tableau=generations", "Génération;Nb;Capacités cumulées", "STD-2020;1;ssd=96"},
		{"/clusters/1/dimensionnement?annee=2027&candidat=2&tableau=resultat",
			"Modèle;Révision;À ajouter;Par zone;Règle limitante;Composant limitant;Besoin;Conservé;Obtenu;Surplus;Coût d'acquisition;Coût annuel;Erreur",
			// MIN_TOTAL=3 (contrainte de la fixture) : 3 serveurs de 576 To
			"DENSE-2027;2;3;;Disque HOT;ssd;35;0;1728;1693;126000;29400;"},
	}
	for _, c := range cas {
		sep := "?"
		if strings.Contains(c.route, "?") {
			sep = "&"
		}
		repCSV, err := client.Get(serveur.URL + c.route + sep + "export=csv")
		if err != nil {
			t.Fatal(err)
		}
		if repCSV.StatusCode != http.StatusOK {
			t.Errorf("%s : export csv : attendu 200, obtenu %d : %s", c.route, repCSV.StatusCode, corps(t, repCSV))
			continue
		}
		if ct := repCSV.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
			t.Errorf("%s : export csv : type attendu text/csv, obtenu %q", c.route, ct)
			continue
		}
		csv := corps(t, repCSV)
		if !strings.HasPrefix(csv, c.entete+"\n") {
			t.Errorf("%s : première ligne csv attendue %q, obtenu : %s", c.route, c.entete, csv)
		}
		if c.contient != "" && !strings.Contains(csv, c.contient+"\n") {
			t.Errorf("%s : ligne %q attendue dans le csv : %s", c.route, c.contient, csv)
		}

		repXLSX, err := client.Get(serveur.URL + c.route + sep + "export=xlsx")
		if err != nil {
			t.Fatal(err)
		}
		if ct := repXLSX.Header.Get("Content-Type"); repXLSX.StatusCode != http.StatusOK || !strings.Contains(ct, "spreadsheetml") {
			t.Errorf("%s : export xlsx : attendu 200 spreadsheetml, obtenu %d %q", c.route, repXLSX.StatusCode, ct)
			continue
		}
		if !strings.HasPrefix(corps(t, repXLSX), "PK") {
			t.Errorf("%s : export xlsx : archive zip attendue", c.route)
		}
	}
}

// TestExportLiensDansLesFragments vérifie la règle des fragments (tableau.go) :
// un tableau rechargé en htmx avec des filtres pose ses boutons dans le
// fragment, avec des liens vers l'URL de la PAGE qui reprennent ces filtres —
// c'est ce qui garantit qu'un export après un clic sur un filtre ou sur la
// case « inclure les archivés » sort bien ce qui est affiché.
func TestExportLiensDansLesFragments(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	if _, err := d.Base().Exec(`
		INSERT INTO projet (id, code, libelle) VALUES (1, 'LOGS', 'Log Management');
		INSERT INTO environnement (id, code, libelle, ordre) VALUES (1, 'PROD', 'Production', 10);
		INSERT INTO techno (id, code, libelle) VALUES (1, 'ELASTIC', 'Elasticsearch');
		INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id) VALUES (1, 'ElasticHot', 1, 1, 1);
		INSERT INTO scenario (id, nom, description, statut, date_creation) VALUES (1, 'Renfort', 'test', 'ACTIF', '2026-01-01');
		INSERT INTO modele (id, type, annee, code) VALUES (2, 'DENSE', 2027, 'DENSE-2027');
	`); err != nil {
		t.Fatalf("fixture : %v", err)
	}

	cas := []struct{ fragment, lien string }{
		{"/projets/tableau?inclure_inactifs=on", `href="/projets?export=csv&amp;inclure_inactifs=on"`},
		{"/technos/tableau?inclure_inactifs=on", `href="/technos?export=xlsx&amp;inclure_inactifs=on"`},
		{"/clusters/tableau?filtre_techno_id=1&inclure_inactifs=on", `href="/clusters?export=csv&amp;filtre_techno_id=1&amp;inclure_inactifs=on"`},
		{"/serveurs/tableau?filtre_statut=EN_SERVICE&filtre_scenario_id=1", `href="/serveurs?export=csv&amp;filtre_scenario_id=1&amp;filtre_statut=EN_SERVICE"`},
		{"/clusters/1/contraintes?annee=2027&scenario=1", `href="/clusters/1/besoin-offre?annee=2027&amp;export=csv&amp;scenario=1&amp;tableau=contraintes"`},
		{"/clusters/1/contraintes?annee=2027&scenario=1", `href="/clusters/1/besoin-offre?annee=2027&amp;export=csv&amp;scenario=1&amp;tableau=contraintes_effectives"`},
		{"/clusters/1/dimensionnement/resultat?annee=2027&scenario=1&candidat=2&conserver=1", `href="/clusters/1/dimensionnement?annee=2027&amp;candidat=2&amp;conserver=1&amp;export=csv&amp;scenario=1&amp;tableau=resultat"`},
	}
	for _, c := range cas {
		rep, err := client.Get(serveur.URL + c.fragment)
		if err != nil {
			t.Fatal(err)
		}
		if page := corps(t, rep); !strings.Contains(page, c.lien) {
			t.Errorf("%s : lien d'export attendu %s dans le fragment : %s", c.fragment, c.lien, page)
		}
	}
}

// TestToutTableauALesBoutonsExport parcourt les gabarits : tout fichier qui
// rend un <table> doit poser {{template "export_boutons" …}}, sauf exception
// motivée ci-dessus.
func TestToutTableauALesBoutonsExport(t *testing.T) {
	var manquants []string
	err := fs.WalkDir(fichiersGabarits, "templates", func(chemin string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(chemin, ".html") {
			return err
		}
		contenu, err := fs.ReadFile(fichiersGabarits, chemin)
		if err != nil {
			return err
		}
		texte := string(contenu)
		if !strings.Contains(texte, "<table") {
			return nil
		}
		if _, exempte := gabaritsSansExport[chemin]; exempte {
			return nil
		}
		aLesBoutons := strings.Contains(texte, `{{template "export_boutons"`)
		if gabaritsAConvertir[chemin] {
			if aLesBoutons {
				t.Errorf("%s est converti : retirer son entrée de gabaritsAConvertir", chemin)
			}
			return nil
		}
		if !aLesBoutons {
			manquants = append(manquants, chemin)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(manquants)
	if len(manquants) > 0 {
		t.Fatalf("gabarits avec un tableau sans boutons d'export (backlog v3.0) :\n  %s", strings.Join(manquants, "\n  "))
	}
}
