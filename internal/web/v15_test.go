package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
)

// serveurDeTestV15 est l'équivalent de serveurDeTest (web_test.go) mais ne
// câble que les routes du lot v1.5 (métriques, variables, règles), plus la
// connexion — le câblage complet vit dans server.go (routes()), hors
// périmètre de ce lot et potentiellement en cours de modification par
// d'autres écrans en parallèle ; ce fichier n'y touche jamais. clientConnecte
// et corps (web_test.go) sont réutilisés tels quels : ils ne dépendent que du
// cookiejar et du corps de la réponse, pas du jeu de routes.
func serveurDeTestV15(t *testing.T) (*httptest.Server, *depot.Depot) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "web-test-v15.db")
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatalf("ouverture de la base de test : %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })

	d := depot.Nouveau(base)
	hash, err := auth.HacherMotDePasse("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerUtilisateur(depot.Utilisateur{
		Login: "gnocent", Hash: hash, Role: depot.RoleEditeur,
	}); err != nil {
		t.Fatal(err)
	}

	svc := auth.NouveauService(d)
	s := &serveur{mux: http.NewServeMux(), gabarits: chargerGabarits(), depot: d, auth: svc}
	s.mux.HandleFunc("GET /connexion", s.connexionFormulaire)
	s.mux.HandleFunc("POST /connexion", s.connexionSoumettre)
	s.mux.HandleFunc("POST /deconnexion", s.exigerAuth(s.verifierCSRF(s.deconnexionSoumettre)))
	s.routesMetriques()
	s.routesVariables()
	s.routesRegles()

	serveur := httptest.NewServer(recuperer(journaliser(s.chargerSession(s.mux))))
	t.Cleanup(serveur.Close)
	return serveur, d
}

// jetonCSRFV15 récupère le jeton depuis une page quelconque, déjà connectée —
// même technique que TestParcoursProjetCompletParHTTP (web_test.go).
func jetonCSRFV15(t *testing.T, client *http.Client, page string) string {
	t.Helper()
	rep, err := client.Get(page)
	if err != nil {
		t.Fatal(err)
	}
	corpsPage := corps(t, rep)
	i := strings.Index(corpsPage, `X-CSRF-Token":"`)
	if i < 0 {
		t.Fatal("jeton CSRF absent de la page " + page)
	}
	i += len(`X-CSRF-Token":"`)
	jeton := corpsPage[i : strings.Index(corpsPage[i:], `"`)+i]
	if jeton == "" {
		t.Fatal("jeton CSRF vide")
	}
	return jeton
}

func requeteFormulaireV15(t *testing.T, client *http.Client, methode, cible, jeton string, valeurs url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(methode, cible, strings.NewReader(valeurs.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// ---------------------------------------------------------------- métriques

func TestMetriquesCRUDParHTTP(t *testing.T) {
	serveur, d := serveurDeTestV15(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFV15(t, client, serveur.URL+"/metriques")

	repCreation := requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/metriques", jeton, url.Values{
		"code": {"DISQUE_UTILE_TO"}, "libelle": {"Disque utile"}, "unite": {"To"},
	})
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if frag := corps(t, repCreation); !strings.Contains(frag, "DISQUE_UTILE_TO") {
		t.Fatalf("la métrique créée devrait apparaître dans le fragment : %s", frag)
	}

	metriques, err := d.ListerMetriques()
	if err != nil {
		t.Fatal(err)
	}
	if len(metriques) != 1 || metriques[0].Code != "DISQUE_UTILE_TO" {
		t.Fatalf("la métrique aurait dû être créée en base : %+v", metriques)
	}
	id := metriques[0].ID

	// modification : libellé et unité changent, le code reste stable
	repMod := requeteFormulaireV15(t, client, http.MethodPut,
		serveur.URL+"/metriques/"+strconv.FormatInt(id, 10), jeton, url.Values{
			"code": {"DISQUE_UTILE_TO"}, "libelle": {"Disque utile (net)"}, "unite": {"To"},
		})
	if repMod.StatusCode != http.StatusOK {
		t.Fatalf("modification : attendu 200, obtenu %d", repMod.StatusCode)
	}
	if frag := corps(t, repMod); !strings.Contains(frag, "Disque utile (net)") {
		t.Fatalf("le libellé modifié devrait apparaître : %s", frag)
	}

	relue, err := d.LireMetrique(id)
	if err != nil {
		t.Fatal(err)
	}
	if relue.Libelle != "Disque utile (net)" {
		t.Fatalf("la modification aurait dû être persistée : %+v", relue)
	}

	// conflit de code : 200 (pas 4xx), message inline, aucune deuxième ligne créée
	repConflit := requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/metriques", jeton, url.Values{
		"code": {"DISQUE_UTILE_TO"}, "libelle": {"doublon"}, "unite": {"To"},
	})
	if repConflit.StatusCode != http.StatusOK {
		t.Fatalf("conflit métier : attendu 200, obtenu %d", repConflit.StatusCode)
	}
	if !strings.Contains(corps(t, repConflit), "existe déjà") {
		t.Fatal("le message de conflit devrait apparaître dans le fragment")
	}
	metriquesApres, err := d.ListerMetriques()
	if err != nil {
		t.Fatal(err)
	}
	if len(metriquesApres) != 1 {
		t.Fatalf("le doublon n'aurait pas dû être créé : %+v", metriquesApres)
	}
}

// ---------------------------------------------------------------- variables

func TestVariablesCRUDEtValeursParHTTP(t *testing.T) {
	serveur, d := serveurDeTestV15(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFV15(t, client, serveur.URL+"/variables")

	// création de la déclaration
	repCreation := requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/variables", jeton, url.Values{
		"code": {"debit_jour_to"}, "libelle": {"Débit journalier entrant"}, "unite": {"To/j"},
		"defaut": {"5"}, "commentaire": {"initial"},
	})
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if frag := corps(t, repCreation); !strings.Contains(frag, "debit_jour_to") {
		t.Fatalf("la variable créée devrait apparaître dans le fragment : %s", frag)
	}

	variables, err := d.ListerVariables()
	if err != nil {
		t.Fatal(err)
	}
	if len(variables) != 1 || variables[0].Code != "debit_jour_to" {
		t.Fatalf("la variable aurait dû être créée en base : %+v", variables)
	}
	id := variables[0].ID

	// page de détail accessible, déclaration affichée
	repDetail, err := client.Get(serveur.URL + "/variables/" + strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	if repDetail.StatusCode != http.StatusOK {
		t.Fatalf("détail : attendu 200, obtenu %d", repDetail.StatusCode)
	}
	if page := corps(t, repDetail); !strings.Contains(page, "debit_jour_to") {
		t.Fatalf("la déclaration devrait apparaître sur la page de détail : %s", page)
	}

	// modification de la déclaration (PUT /variables/{id})
	repModDecl := requeteFormulaireV15(t, client, http.MethodPut,
		serveur.URL+"/variables/"+strconv.FormatInt(id, 10), jeton, url.Values{
			"code": {"debit_jour_to"}, "libelle": {"Débit journalier (révisé)"}, "unite": {"To/j"},
			"defaut": {"7"}, "commentaire": {"révisé"},
		})
	if repModDecl.StatusCode != http.StatusOK {
		t.Fatalf("modification de la déclaration : attendu 200, obtenu %d", repModDecl.StatusCode)
	}
	if frag := corps(t, repModDecl); !strings.Contains(frag, "Débit journalier (révisé)") {
		t.Fatalf("le libellé modifié devrait apparaître : %s", frag)
	}

	// définition d'une valeur pour le réel, année 2025, portée globale
	requeteValeur := func(valeur string) *http.Response {
		return requeteFormulaireV15(t, client, http.MethodPost,
			serveur.URL+"/variables/"+strconv.FormatInt(id, 10)+"/valeurs", jeton, url.Values{
				"annee": {"2025"}, "valeur": {valeur}, "commentaire": {"note"},
			})
	}
	repValeur1 := requeteValeur("10")
	if repValeur1.StatusCode != http.StatusOK {
		t.Fatalf("définition de la valeur : attendu 200, obtenu %d", repValeur1.StatusCode)
	}
	if frag := corps(t, repValeur1); !strings.Contains(frag, "10") {
		t.Fatalf("la valeur créée devrait apparaître dans le fragment : %s", frag)
	}

	valeurs, err := d.ListerValeurs(id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(valeurs) != 1 || valeurs[0].Valeur != 10 {
		t.Fatalf("la valeur aurait dû être créée en base : %+v", valeurs)
	}
	valeurID := valeurs[0].ID

	// écrasement : même portée exacte, valeur différente -> upsert + historique
	repValeur2 := requeteValeur("20")
	if repValeur2.StatusCode != http.StatusOK {
		t.Fatalf("modification de la valeur : attendu 200, obtenu %d", repValeur2.StatusCode)
	}

	valeursApres, err := d.ListerValeurs(id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(valeursApres) != 1 {
		t.Fatalf("l'écrasement aurait dû mettre à jour la même ligne, pas en créer une autre : %+v", valeursApres)
	}
	if valeursApres[0].ID != valeurID || valeursApres[0].Valeur != 20 {
		t.Fatalf("la valeur aurait dû passer à 20 sur la même ligne : %+v", valeursApres[0])
	}

	// l'historique montre l'écrasement 10 -> 20
	repHisto, err := client.Get(serveur.URL + "/variables/" + strconv.FormatInt(id, 10) +
		"/valeurs/" + strconv.FormatInt(valeurID, 10) + "/historique")
	if err != nil {
		t.Fatal(err)
	}
	if repHisto.StatusCode != http.StatusOK {
		t.Fatalf("historique : attendu 200, obtenu %d", repHisto.StatusCode)
	}
	pageHisto := corps(t, repHisto)
	if !strings.Contains(pageHisto, "10") || !strings.Contains(pageHisto, "20") {
		t.Fatalf("l'historique devrait montrer l'ancienne (10) et la nouvelle (20) valeur : %s", pageHisto)
	}

	historique, err := d.HistoriqueValeur(valeurID)
	if err != nil {
		t.Fatal(err)
	}
	if len(historique) != 1 || historique[0].Ancienne != 10 || historique[0].Nouvelle != 20 {
		t.Fatalf("l'historique en base devrait tracer 10 -> 20 : %+v", historique)
	}

	// suppression de la valeur
	repSuppr := requeteFormulaireV15(t, client, http.MethodPost,
		serveur.URL+"/variables/"+strconv.FormatInt(id, 10)+"/valeurs/"+strconv.FormatInt(valeurID, 10)+"/supprimer",
		jeton, url.Values{})
	if repSuppr.StatusCode != http.StatusOK {
		t.Fatalf("suppression : attendu 200, obtenu %d", repSuppr.StatusCode)
	}
	valeursFinales, err := d.ListerValeurs(id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(valeursFinales) != 0 {
		t.Fatalf("la valeur aurait dû être supprimée : %+v", valeursFinales)
	}
}

// TestVariableValeurScenarioParHTTP couvre la v2.1 : une nouvelle valeur créée
// sous un scénario s'écrit dans son seau (pas dans le réel), la liste sous ce
// scénario montre les deux ensemble, et modifier cette valeur plus tard
// réécrit dans son propre seau même si un autre scénario est alors affiché.
func TestVariableValeurScenarioParHTTP(t *testing.T) {
	serveur, d := serveurDeTestV15(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFV15(t, client, serveur.URL+"/variables")

	if _, err := d.Base().Exec(
		`INSERT INTO scenario (id, nom, description, statut, date_creation)
		 VALUES (1, 'Renfort', 'test', 'ACTIF', '2026-01-01')`); err != nil {
		t.Fatalf("seed scénario : %v", err)
	}

	requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/variables", jeton, url.Values{
		"code": {"debit"}, "libelle": {"Débit"},
	})
	variables, err := d.ListerVariables()
	if err != nil || len(variables) != 1 {
		t.Fatalf("variable de test : %v, %+v", err, variables)
	}
	id := variables[0].ID

	// valeur du réel, année 2027.
	requeteFormulaireV15(t, client, http.MethodPost,
		serveur.URL+"/variables/"+strconv.FormatInt(id, 10)+"/valeurs", jeton, url.Values{
			"annee": {"2027"}, "valeur": {"5"},
		})

	// nouvelle valeur sous le scénario, même année : le champ caché "scenario"
	// du formulaire "Nouvelle valeur" porte le scénario affiché.
	repScenario := requeteFormulaireV15(t, client, http.MethodPost,
		serveur.URL+"/variables/"+strconv.FormatInt(id, 10)+"/valeurs?scenario=1", jeton, url.Values{
			"annee": {"2027"}, "valeur": {"10"}, "scenario": {"1"},
		})
	if repScenario.StatusCode != http.StatusOK {
		t.Fatalf("valeur sous scénario : attendu 200, obtenu %d", repScenario.StatusCode)
	}

	// le réel n'a qu'une valeur, inchangée.
	reel, err := d.ListerValeurs(id, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reel) != 1 || reel[0].Valeur != 5 {
		t.Fatalf("le réel doit rester à 5, une seule ligne : %+v", reel)
	}

	// sous le scénario, la lecture brute montre les deux.
	sc := int64(1)
	avecSc, err := d.ListerValeurs(id, &sc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(avecSc) != 2 {
		t.Fatalf("sous le scénario, réel et surcharge doivent apparaître ensemble : %+v", avecSc)
	}

	// la page, sous ce scénario, montre bien les deux badges.
	repPage, err := client.Get(serveur.URL + "/variables/" + strconv.FormatInt(id, 10) + "?scenario=1")
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, repPage)
	if !strings.Contains(page, "badge-actif\">réel") || !strings.Contains(page, "badge\">scénario") {
		t.Fatalf("la page sous scénario doit montrer les deux badges réel et scénario : %s", page)
	}
}

// ---------------------------------------------------------------- règles

// preparerReferentielsRegles crée directement en base (hors HTTP, pas l'objet
// de ce lot) le strict nécessaire pour qu'une règle ait un domaine
// d'application concret : un projet, un environnement, une techno et un
// cluster qui les croise. DetecterConflitsRegles (internal/depot/regle.go)
// ne peut détecter un recouvrement qu'entre règles qui matchent un cluster
// réel — sans cluster, deux règles sans filtre ne "se recouvrent" jamais.
func preparerReferentielsRegles(t *testing.T, d *depot.Depot) (metriqueID, clusterID int64) {
	t.Helper()
	m, err := d.CreerMetrique(depot.Metrique{Code: "CPU_CORES", Libelle: "Cœurs CPU", Unite: "core"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Log Management"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := d.CreerEnvironnement(depot.Environnement{Code: "PROD", Libelle: "Production", Ordre: 1})
	if err != nil {
		t.Fatal(err)
	}
	techno, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := d.CreerCluster(depot.Cluster{
		Nom: "ElasticHot1", ProjetID: p.ID, EnvironnementID: e.ID, TechnoID: techno.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m.ID, c.ID
}

func TestReglesCRUDParHTTP(t *testing.T) {
	serveur, d := serveurDeTestV15(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFV15(t, client, serveur.URL+"/regles")

	metriqueID, _ := preparerReferentielsRegles(t, d)

	repCreation := requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/regles", jeton, url.Values{
		"nom": {"Cœurs Elastic Hot"}, "metrique_id": {strconv.FormatInt(metriqueID, 10)},
		"expression": {"10"}, "niveau_evaluation": {"PERIMETRE"}, "composant_offre": {"cpu"},
	})
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if frag := corps(t, repCreation); !strings.Contains(frag, "Cœurs Elastic Hot") {
		t.Fatalf("la règle créée devrait apparaître dans le fragment : %s", frag)
	}

	regles, err := d.ListerRegles(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(regles) != 1 || !regles[0].Actif {
		t.Fatalf("la règle aurait dû être créée active en base : %+v", regles)
	}
	id := regles[0].ID

	// page de détail accessible
	repDetail, err := client.Get(serveur.URL + "/regles/" + strconv.FormatInt(id, 10))
	if err != nil {
		t.Fatal(err)
	}
	if repDetail.StatusCode != http.StatusOK {
		t.Fatalf("détail : attendu 200, obtenu %d", repDetail.StatusCode)
	}

	// désactivation puis réactivation (rien à recouvrir, seule règle du jeu)
	repDesactiver := requeteFormulaireV15(t, client, http.MethodPost,
		serveur.URL+"/regles/"+strconv.FormatInt(id, 10)+"/desactiver", jeton, url.Values{})
	if repDesactiver.StatusCode != http.StatusOK {
		t.Fatalf("désactivation : attendu 200, obtenu %d", repDesactiver.StatusCode)
	}
	desactivee, err := d.LireRegle(id)
	if err != nil {
		t.Fatal(err)
	}
	if desactivee.Actif {
		t.Fatal("la règle aurait dû être désactivée")
	}

	repActiver := requeteFormulaireV15(t, client, http.MethodPost,
		serveur.URL+"/regles/"+strconv.FormatInt(id, 10)+"/activer", jeton, url.Values{})
	if repActiver.StatusCode != http.StatusOK {
		t.Fatalf("activation : attendu 200, obtenu %d", repActiver.StatusCode)
	}
	reactivee, err := d.LireRegle(id)
	if err != nil {
		t.Fatal(err)
	}
	if !reactivee.Actif {
		t.Fatal("la règle aurait dû être réactivée")
	}

	// duplication : copie inactive nommée "... (copie)", visible dans la liste entière
	repDupliquer := requeteFormulaireV15(t, client, http.MethodPost,
		serveur.URL+"/regles/"+strconv.FormatInt(id, 10)+"/dupliquer", jeton, url.Values{})
	if repDupliquer.StatusCode != http.StatusOK {
		t.Fatalf("duplication : attendu 200, obtenu %d", repDupliquer.StatusCode)
	}
	if frag := corps(t, repDupliquer); !strings.Contains(frag, "(copie)") {
		t.Fatalf("la copie devrait apparaître dans le tableau renvoyé : %s", frag)
	}
	reglesApresDup, err := d.ListerRegles(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(reglesApresDup) != 2 {
		t.Fatalf("la duplication aurait dû ajouter une deuxième règle : %+v", reglesApresDup)
	}

	// modification complète via la page de détail
	repMod := requeteFormulaireV15(t, client, http.MethodPut,
		serveur.URL+"/regles/"+strconv.FormatInt(id, 10), jeton, url.Values{
			"nom": {"Cœurs Elastic Hot (révisé)"}, "metrique_id": {strconv.FormatInt(metriqueID, 10)},
			"expression": {"12"}, "niveau_evaluation": {"PERIMETRE"}, "composant_offre": {"cpu"},
			"actif": {"1"},
		})
	if repMod.StatusCode != http.StatusOK {
		t.Fatalf("modification : attendu 200, obtenu %d", repMod.StatusCode)
	}
	if frag := corps(t, repMod); !strings.Contains(frag, "Cœurs Elastic Hot (révisé)") {
		t.Fatalf("le nom modifié devrait apparaître : %s", frag)
	}
	relue, err := d.LireRegle(id)
	if err != nil {
		t.Fatal(err)
	}
	if relue.Expression != "12" || !relue.Actif {
		t.Fatalf("la modification aurait dû être persistée, règle toujours active : %+v", relue)
	}
}

// TestReglesRecouvrementRefuse est le test clé du lot : deux règles actives
// sur la même métrique dont le filtre matche un même cluster ne peuvent pas
// coexister (docs/modele-donnees.md §12 invariant 2, CLAUDE.md « Pas
// d'ambiguïté silencieuse »).
func TestReglesRecouvrementRefuse(t *testing.T) {
	serveur, d := serveurDeTestV15(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFV15(t, client, serveur.URL+"/regles")

	metriqueID, _ := preparerReferentielsRegles(t, d)

	repA := requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/regles", jeton, url.Values{
		"nom": {"Règle A"}, "metrique_id": {strconv.FormatInt(metriqueID, 10)},
		"expression": {"10"}, "niveau_evaluation": {"PERIMETRE"}, "composant_offre": {"cpu"},
	})
	if repA.StatusCode != http.StatusOK {
		t.Fatalf("création de la règle A : attendu 200, obtenu %d", repA.StatusCode)
	}

	// règle B : même métrique, aucun filtre -> recouvre A sur ElasticHot1.
	// Refusée : 200 (jamais 4xx sur un endpoint htmx), message nommant A,
	// et B n'apparaît pas en base.
	repB := requeteFormulaireV15(t, client, http.MethodPost, serveur.URL+"/regles", jeton, url.Values{
		"nom": {"Règle B"}, "metrique_id": {strconv.FormatInt(metriqueID, 10)},
		"expression": {"20"}, "niveau_evaluation": {"PERIMETRE"}, "composant_offre": {"cpu"},
	})
	if repB.StatusCode != http.StatusOK {
		t.Fatalf("recouvrement refusé : attendu 200 (htmx n'affiche pas les 4xx), obtenu %d", repB.StatusCode)
	}
	fragB := corps(t, repB)
	if !strings.Contains(fragB, "Règle A") {
		t.Fatalf("le message de refus devrait nommer la règle concurrente « Règle A » : %s", fragB)
	}
	if !strings.Contains(fragB, "recouvre") {
		t.Fatalf("le message devrait expliquer le recouvrement : %s", fragB)
	}

	regles, err := d.ListerRegles(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(regles) != 1 || regles[0].Nom != "Règle A" {
		t.Fatalf("seule la règle A aurait dû exister en base : %+v", regles)
	}
}
