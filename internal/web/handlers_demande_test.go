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

// serveurDemandesDeTest construit un serveur de test sur le modèle de
// serveurImportModelesDeTest : routes communes d'authentification plus les
// deux groupes de cet écran (routesDemandes, routesParametres), que server.go
// ne câble pas encore. Le compte gnocent est ADMIN : l'édition du gabarit
// l'exige, et ADMIN couvre aussi les écritures d'éditeur.
func serveurDemandesDeTest(t *testing.T) (*httptest.Server, *depot.Depot) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "web-test-demandes.db")
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
		Login: "gnocent", Hash: hash, Role: depot.RoleAdmin,
	}); err != nil {
		t.Fatal(err)
	}

	svc := auth.NouveauService(d)
	s := &serveur{mux: http.NewServeMux(), gabarits: chargerGabarits(), depot: d, auth: svc}
	s.mux.HandleFunc("GET /connexion", s.connexionFormulaire)
	s.mux.HandleFunc("POST /connexion", s.connexionSoumettre)
	s.mux.HandleFunc("POST /deconnexion", s.exigerAuth(s.verifierCSRF(s.deconnexionSoumettre)))
	s.routesDemandes()
	s.routesParametres()

	handler := recuperer(journaliser(s.chargerSession(s.mux)))
	serveurHTTP := httptest.NewServer(handler)
	t.Cleanup(serveurHTTP.Close)
	return serveurHTTP, d
}

// parcDemandesDeTest sème un parc minimal : un cluster complet, une révision
// avec composants, un serveur réel renseigné et affecté, un serveur réel nu,
// et une hypothèse affectée dans son scénario. Renvoie (réel complet, réel
// nu, hypothèse, scénario).
func parcDemandesDeTest(t *testing.T, d *depot.Depot) (complet, nu, hypothese, scenario int64) {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := d.CreerProjet(depot.Projet{Code: "LOGS", Libelle: "Logs"})
	must(err)
	e, err := d.CreerEnvironnement(depot.Environnement{Code: "PROD", Libelle: "Production"})
	must(err)
	te, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	must(err)
	z, err := d.CreerZone(depot.Zone{Code: "DC1", Libelle: "Zone 1", Site: ptrTexte("Paris")})
	must(err)
	c, err := d.CreerCluster(depot.Cluster{Nom: "ElasticCold1", ProjetID: p.ID, EnvironnementID: e.ID, TechnoID: te.ID})
	must(err)
	m, err := d.CreerModele(depot.Modele{Type: "DENSE", Annee: 2025, Code: "DENSE-2025"})
	must(err)
	rev, err := d.CreerRevision(depot.Revision{ModeleID: m.ID, DateEffet: "2025-01-15"})
	must(err)
	for _, comp := range []depot.Composant{
		{RevisionID: rev.ID, Nature: "CPU", Code: "cpu", Quantite: 1, CapaciteUnitaire: 64, Unite: "CORE"},
		{RevisionID: rev.ID, Nature: "RAM", Code: "ram", Quantite: 1, CapaciteUnitaire: 1024, Unite: "GO"},
		{RevisionID: rev.ID, Nature: "DISQUE_DATA", Code: "ssd", Quantite: 24, CapaciteUnitaire: 7.68, Unite: "TO"},
	} {
		_, err := d.AjouterComposant(comp, false)
		must(err)
	}

	s, err := d.CreerServeur(depot.Serveur{
		PhysicalName: ptrTexte("PHY001"), Hostname: ptrTexte("esh01"), IP: ptrTexte("10.0.0.1"),
		ZoneID: &z.ID, DemandeRef: ptrTexte("D-100"), Statut: depot.StatutEnService,
	})
	must(err)
	must(d.RattacherRevision(s.ID, rev.ID, "2025-03-01", nil))
	must(d.Affecter(s.ID, c.ID, "2025-03-01", nil, nil))

	n, err := d.CreerServeur(depot.Serveur{PhysicalName: ptrTexte("PHY002"), Statut: depot.StatutCommande})
	must(err)

	sc, err := d.CreerScenario(depot.Scenario{Nom: "Achat 2027", Description: "test"})
	must(err)
	must(d.ActiverScenario(sc.ID))
	h, err := d.CreerServeur(depot.Serveur{
		PhysicalName: ptrTexte("HYP001"), Statut: depot.StatutHypothese, ScenarioID: &sc.ID,
	})
	must(err)
	must(d.RattacherRevision(h.ID, rev.ID, "2027-01-01", nil))
	must(d.Affecter(h.ID, c.ID, "2027-01-01", &sc.ID, nil))
	return s.ID, n.ID, h.ID, sc.ID
}

func ptrTexte(s string) *string { return &s }

// posterFormulaire envoie un formulaire classique (application/x-www-form-
// urlencoded) avec le jeton CSRF en champ _csrf, comme un <form method=post>.
func posterFormulaire(t *testing.T, client *http.Client, adresse, jeton string, form url.Values) *http.Response {
	t.Helper()
	form.Set("_csrf", jeton)
	req, err := http.NewRequest(http.MethodPost, adresse, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func lirePage(t *testing.T, client *http.Client, adresse string) (int, string) {
	t.Helper()
	rep, err := client.Get(adresse)
	if err != nil {
		t.Fatal(err)
	}
	return rep.StatusCode, corps(t, rep)
}

const gabaritDeTest = "Serveur {nom_physique} ({hostname}) fiche {num_fiche}\n" +
	"Cluster {cluster} — {modele} rév. {revision} : {cpu} cœurs, {ram} Go, {ssd} To SSD, {{brut}}\n" +
	"IP {ip} demande {num_demande}"

func TestDemandesParcoursComplet(t *testing.T) {
	serveur, d := serveurDemandesDeTest(t)
	complet, nu, hypothese, sc := parcDemandesDeTest(t, d)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/demandes")

	// sans gabarit : la page l'annonce et renvoie vers son édition, les
	// serveurs sont tout de même listés.
	code, page := lirePage(t, client, serveur.URL+"/demandes")
	if code != http.StatusOK {
		t.Fatalf("GET /demandes : attendu 200, obtenu %d", code)
	}
	if !strings.Contains(page, "Aucun gabarit") || !strings.Contains(page, `href="/parametres/gabarit"`) {
		t.Fatalf("sans gabarit, la page doit le dire et lier /parametres/gabarit : %s", page)
	}
	if !strings.Contains(page, "PHY001") || !strings.Contains(page, "PHY002") || strings.Contains(page, "HYP001") {
		t.Fatalf("le réel doit lister PHY001 et PHY002 sans l'hypothèse : %s", page)
	}

	// gabarit à variable inconnue : refusé en la nommant, rien d'enregistré.
	repRefus := posterFormulaire(t, client, serveur.URL+"/parametres/gabarit", jeton,
		url.Values{"gabarit": {"Serveur {nom_physique} type {typo}"}})
	if repRefus.StatusCode != http.StatusOK {
		t.Fatalf("refus de gabarit : attendu 200 avec message inline, obtenu %d", repRefus.StatusCode)
	}
	if page := corps(t, repRefus); !strings.Contains(page, "variable inconnue « typo »") || !strings.Contains(page, "{typo}") {
		t.Fatalf("le refus doit nommer la variable et conserver la saisie : %s", page)
	}
	if _, present, _ := d.LireParametre(depot.ParametreGabaritDemande); present {
		t.Fatal("un gabarit refusé ne doit pas être enregistré")
	}

	// gabarit valide : enregistré, aperçu rendu sur le premier serveur.
	repOK := posterFormulaire(t, client, serveur.URL+"/parametres/gabarit", jeton,
		url.Values{"gabarit": {strings.ReplaceAll(gabaritDeTest, "\n", "\r\n")}})
	pageOK := corps(t, repOK)
	if !strings.Contains(pageOK, "Gabarit enregistré") || !strings.Contains(pageOK, "Serveur PHY001 (esh01)") {
		t.Fatalf("l'enregistrement doit confirmer et afficher un aperçu : %s", pageOK)
	}
	texte, present, _ := d.LireParametre(depot.ParametreGabaritDemande)
	if !present || texte != gabaritDeTest {
		t.Fatalf("le gabarit doit être enregistré en LF, obtenu present=%v %q", present, texte)
	}
	code, pageGabarit := lirePage(t, client, serveur.URL+"/parametres/gabarit")
	if code != http.StatusOK || !strings.Contains(pageGabarit, "{nom_physique}") || !strings.Contains(pageGabarit, "Numéro de fiche") {
		t.Fatalf("GET /parametres/gabarit doit montrer le gabarit courant et l'aide-mémoire : %d %s", code, pageGabarit)
	}

	// la page des demandes rend le bloc avec les vraies valeurs.
	_, page = lirePage(t, client, serveur.URL+"/demandes")
	attendu := "Serveur PHY001 (esh01) fiche \nCluster ElasticCold1 — DENSE-2025 rév. 1 : 64 cœurs, 1024 Go, 184.32 To SSD, {brut}\nIP 10.0.0.1 demande D-100"
	if !strings.Contains(page, attendu) {
		t.Fatalf("bloc attendu :\n%s\ndans la page :\n%s", attendu, page)
	}
	if !strings.Contains(page, `hx-put="/demandes/`+itoa(complet)+`/fiche"`) || !strings.Contains(page, "copierBloc") {
		t.Fatal("un éditeur doit voir l'input du numéro de fiche et le bouton copier")
	}

	// édition du numéro de fiche : la ligne renvoyée porte le bloc recalculé.
	req, _ := http.NewRequest(http.MethodPut, serveur.URL+"/demandes/"+itoa(complet)+"/fiche",
		strings.NewReader(url.Values{"fiche": {"F-42"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	repFiche, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	fragment := corps(t, repFiche)
	if repFiche.StatusCode != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(fragment), "<tr") {
		t.Fatalf("PUT fiche : attendu une ligne 200, obtenu %d %s", repFiche.StatusCode, fragment)
	}
	if !strings.Contains(fragment, `value="F-42"`) || !strings.Contains(fragment, "fiche F-42\n") {
		t.Fatalf("la ligne doit porter le numéro et le bloc recalculé : %s", fragment)
	}
	if s, _ := d.LireServeur(complet); s.DemandeServeurRef == nil || *s.DemandeServeurRef != "F-42" {
		t.Fatal("le numéro de fiche doit être en base")
	}

	// numéro de demande en masse sur les serveurs affichés, filtres rejoués.
	repMasse := posterFormulaire(t, client, serveur.URL+"/demandes/numero", jeton, url.Values{
		"numero": {"D-500"}, "id": {itoa(complet), itoa(nu)}, "statut": {""}, "sans_numero": {"1"},
	})
	repMasse.Body.Close()
	if repMasse.StatusCode != http.StatusSeeOther {
		t.Fatalf("numéro en masse : attendu 303, obtenu %d", repMasse.StatusCode)
	}
	suite := repMasse.Header.Get("Location")
	if !strings.Contains(suite, "affectes=2") || !strings.Contains(suite, "sans_numero=1") {
		t.Fatalf("la redirection doit rejouer les filtres et compter les serveurs : %s", suite)
	}
	for _, id := range []int64{complet, nu} {
		if s, _ := d.LireServeur(id); s.DemandeRef == nil || *s.DemandeRef != "D-500" {
			t.Fatalf("serveur %d : numéro D-500 attendu en base", id)
		}
	}
	if s, _ := d.LireServeur(hypothese); s.DemandeRef != nil {
		t.Fatal("l'hypothèse non affichée ne doit pas recevoir le numéro")
	}
	_, pageSuite := lirePage(t, client, serveur.URL+suite)
	if !strings.Contains(pageSuite, "affecté à 2 serveur(s)") {
		t.Fatalf("la page doit confirmer l'affectation : %s", pageSuite)
	}

	// filtre « sans numéro » : plus personne dans le réel ; sous le
	// scénario, l'hypothèse seule.
	_, pageSans := lirePage(t, client, serveur.URL+"/demandes?sans_numero=1")
	if strings.Contains(pageSans, "PHY001") || strings.Contains(pageSans, "PHY002") || !strings.Contains(pageSans, "Aucun serveur") {
		t.Fatalf("sans numéro : aucun serveur réel attendu : %s", pageSans)
	}
	_, pageScenario := lirePage(t, client, serveur.URL+"/demandes?scenario="+itoa(sc)+"&sans_numero=1")
	if !strings.Contains(pageScenario, "HYP001") || strings.Contains(pageScenario, "PHY001") {
		t.Fatalf("sous scénario : l'hypothèse seule attendue : %s", pageScenario)
	}
	if !strings.Contains(pageScenario, "Achat 2027") {
		t.Fatal("le bloc de l'hypothèse doit pouvoir nommer son scénario")
	}
	_, pageDemande := lirePage(t, client, serveur.URL+"/demandes?demande=D-500&cluster=1")
	if !strings.Contains(pageDemande, "PHY001") || strings.Contains(pageDemande, "PHY002") {
		t.Fatalf("filtre numéro + cluster : PHY001 seul attendu : %s", pageDemande)
	}

	// export csv : mêmes filtres, description = le bloc.
	repCSV, err := client.Get(serveur.URL + "/demandes?export=csv&cluster=1")
	if err != nil {
		t.Fatal(err)
	}
	if ct := repCSV.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("export csv : type attendu text/csv, obtenu %q", ct)
	}
	csv := corps(t, repCSV)
	if !strings.HasPrefix(csv, "Serveur;Cluster;Statut;Numéro de demande;Numéro de fiche;Description\n") {
		t.Fatalf("en-tête csv inattendu : %s", csv)
	}
	if !strings.Contains(csv, "PHY001;ElasticCold1;EN_SERVICE;D-500;F-42;\"Serveur PHY001 (esh01) fiche F-42\n") || strings.Contains(csv, "PHY002") {
		t.Fatalf("contenu csv inattendu : %s", csv)
	}
}

// TestDemandesLecteurNeModifiePas : un lecteur voit les blocs mais ni
// l'input du numéro de fiche ni l'action en masse ; ses écritures sont
// refusées.
func TestDemandesLecteurNeModifiePas(t *testing.T) {
	serveur, d := serveurDemandesDeTest(t)
	complet, _, _, _ := parcDemandesDeTest(t, d)
	if err := d.DefinirParametre(depot.ParametreGabaritDemande, "Serveur {nom_physique}", nil); err != nil {
		t.Fatal(err)
	}
	client := clientConnecte(t, serveur)
	jeton := jetonCSRFDepuisPage(t, client, serveur, "/demandes")

	us, _ := d.ListerUtilisateurs(false)
	us[0].Role = depot.RoleLecteur
	if err := d.ModifierUtilisateur(us[0]); err != nil {
		t.Fatal(err)
	}

	code, page := lirePage(t, client, serveur.URL+"/demandes")
	if code != http.StatusOK || !strings.Contains(page, "Serveur PHY001") {
		t.Fatalf("un lecteur consulte les blocs : %d %s", code, page)
	}
	if strings.Contains(page, "hx-put=") || strings.Contains(page, `action="/demandes/numero"`) {
		t.Fatal("un lecteur ne doit voir ni l'input de fiche ni l'action en masse")
	}

	req, _ := http.NewRequest(http.MethodPut, serveur.URL+"/demandes/"+itoa(complet)+"/fiche",
		strings.NewReader(url.Values{"fiche": {"F-1"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jeton)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rep.Body.Close()
	if rep.StatusCode != http.StatusForbidden {
		t.Fatalf("PUT fiche par un lecteur : attendu 403, obtenu %d", rep.StatusCode)
	}
	if code, _ := lirePage(t, client, serveur.URL+"/parametres/gabarit"); code != http.StatusForbidden {
		t.Fatalf("le gabarit est réservé à l'administrateur : attendu 403, obtenu %d", code)
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
