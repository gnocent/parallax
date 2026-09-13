package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"parallax/internal/depot"
)

// envoyerFormulaireModele envoie un formulaire url-encodé avec le jeton CSRF
// — factorise le patron répété dans TestParcoursProjetCompletParHTTP
// (web_test.go). Nom distinct de requeteFormulaire (handlers_cluster_test.go,
// hors périmètre de cet écran, signature différente) pour éviter toute
// redéclaration dans le paquet.
func envoyerFormulaireModele(t *testing.T, client *http.Client, methode, cible, jeton string, valeurs url.Values) *http.Response {
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

// TestParcoursModeleCompletParHTTP suit le patron de
// TestParcoursProjetCompletParHTTP (web_test.go) : création d'un modèle,
// création d'une révision, ajout d'un composant, puis LE TEST CLÉ —
// l'immuabilité d'une révision référencée par du matériel en service
// (docs/modele-donnees.md §12 invariant 1, CLAUDE.md).
func TestParcoursModeleCompletParHTTP(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/modeles")

	// création du modèle : seuls type, année, code et financement se
	// saisissent ici (les champs financiers vivent sur la page de détail).
	repCreation := envoyerFormulaireModele(t, client, http.MethodPost, serveur.URL+"/modeles", jeton, url.Values{
		"type": {"DENSE"}, "annee": {"2025"}, "code": {"DENSE-2025"}, "mode_financement": {"LEASE"},
	})
	if repCreation.StatusCode != http.StatusOK {
		t.Fatalf("création modèle : attendu 200, obtenu %d", repCreation.StatusCode)
	}
	if frag := corps(t, repCreation); !strings.Contains(frag, "DENSE-2025") {
		t.Fatalf("le modèle créé devrait apparaître dans le fragment : %s", frag)
	}

	modeles, err := d.ListerModeles(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(modeles) != 1 || modeles[0].Code != "DENSE-2025" {
		t.Fatalf("le modèle aurait dû être créé en base : %+v", modeles)
	}
	modeleID := modeles[0].ID
	modeleURL := serveur.URL + "/modeles/" + strconv.FormatInt(modeleID, 10)

	// la page de détail est une navigation classique, pas un fragment htmx
	repDetail, err := client.Get(modeleURL)
	if err != nil {
		t.Fatal(err)
	}
	if repDetail.StatusCode != http.StatusOK {
		t.Fatalf("page de détail : attendu 200, obtenu %d", repDetail.StatusCode)
	}
	if page := corps(t, repDetail); !strings.Contains(page, "DENSE-2025") {
		t.Fatalf("le code du modèle devrait apparaître sur sa page de détail : %s", page)
	}

	// une révision inexistante : 404 classique (navigation, pas un endpoint htmx)
	repIntrouvable, err := client.Get(serveur.URL + "/modeles/999999")
	if err != nil {
		t.Fatal(err)
	}
	if repIntrouvable.StatusCode != http.StatusNotFound {
		t.Fatalf("modèle inexistant : attendu 404, obtenu %d", repIntrouvable.StatusCode)
	}
	corps(t, repIntrouvable)

	// création d'une révision
	repRevision := envoyerFormulaireModele(t, client, http.MethodPost, modeleURL+"/revisions", jeton, url.Values{
		"libelle": {"initiale"}, "date_effet": {"2025-01-15"}, "commentaire": {"config d'origine"},
	})
	if repRevision.StatusCode != http.StatusOK {
		t.Fatalf("création révision : attendu 200, obtenu %d", repRevision.StatusCode)
	}
	if frag := corps(t, repRevision); !strings.Contains(frag, "Révision 1") {
		t.Fatalf("la révision créée devrait apparaître dans le fragment : %s", frag)
	}

	revisions, err := d.ListerRevisions(modeleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 {
		t.Fatalf("une révision attendue en base : %+v", revisions)
	}
	revisionID := revisions[0].ID
	revisionURL := modeleURL + "/revisions/" + strconv.FormatInt(revisionID, 10)

	// ajout d'un composant
	repComposant := envoyerFormulaireModele(t, client, http.MethodPost, revisionURL+"/composants", jeton, url.Values{
		"nature": {"DISQUE_DATA"}, "code": {"ssd"},
		"quantite": {"24"}, "capacite_unitaire": {"24"}, "unite": {"TO"},
	})
	if repComposant.StatusCode != http.StatusOK {
		t.Fatalf("ajout composant : attendu 200, obtenu %d", repComposant.StatusCode)
	}
	if frag := corps(t, repComposant); !strings.Contains(frag, "ssd") {
		t.Fatalf("le composant ajouté devrait apparaître dans le fragment : %s", frag)
	}

	composants, err := d.ListerComposants(revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(composants) != 1 || composants[0].Code != "ssd" {
		t.Fatalf("le composant aurait dû être créé en base : %+v", composants)
	}

	// LE TEST CLÉ : rendre la révision référencée par du matériel en service,
	// par SQL direct — même technique que referencerRevision
	// (internal/depot/modele_test.go, utilisée par TestRevisionImmuabilite).
	res, err := d.Base().Exec(`INSERT INTO serveur (statut) VALUES ('EN_SERVICE')`)
	if err != nil {
		t.Fatalf("fixture serveur : %v", err)
	}
	serveurID, _ := res.LastInsertId()
	if _, err := d.Base().Exec(
		`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, ?, '2025-02-01')`,
		serveurID, revisionID); err != nil {
		t.Fatalf("fixture serveur_revision : %v", err)
	}
	if referencee, err := d.RevisionEstReferencee(revisionID); err != nil || !referencee {
		t.Fatalf("la révision devrait être référencée : %v / %v", referencee, err)
	}

	// sans la case « corriger » : refus propre (200, message explicite), rien n'est appliqué
	repRefus := envoyerFormulaireModele(t, client, http.MethodPut, revisionURL, jeton, url.Values{
		"libelle": {"tentative sans correction"}, "date_effet": {"2025-03-01"},
	})
	if repRefus.StatusCode != http.StatusOK {
		t.Fatalf("modification sans correction : attendu 200 (htmx n'affiche pas les 4xx), obtenu %d", repRefus.StatusCode)
	}
	fragRefus := corps(t, repRefus)
	if !strings.Contains(fragRefus, "Créez une nouvelle révision") {
		t.Fatalf("le message d'immuabilité devrait apparaître dans le fragment : %s", fragRefus)
	}
	if strings.Contains(fragRefus, "Correction appliquée") {
		t.Fatal("aucun bandeau d'avertissement ne doit apparaître quand la modification est refusée")
	}
	relue, err := d.LireRevision(revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if relue.Libelle == nil || *relue.Libelle != "initiale" || relue.DateEffet != "2025-01-15" {
		t.Fatalf("le refus ne doit rien modifier en base : %+v", relue)
	}

	// avec la case « corriger » cochée : appliqué, bandeau d'avertissement affiché
	repCorrection := envoyerFormulaireModele(t, client, http.MethodPut, revisionURL, jeton, url.Values{
		"libelle": {"correction d'une coquille"}, "date_effet": {"2025-01-15"}, "corriger": {"1"},
	})
	if repCorrection.StatusCode != http.StatusOK {
		t.Fatalf("correction : attendu 200, obtenu %d", repCorrection.StatusCode)
	}
	fragCorrection := corps(t, repCorrection)
	if !strings.Contains(fragCorrection, "Correction appliquée") {
		t.Fatalf("le bandeau d'avertissement devrait apparaître dans le fragment : %s", fragCorrection)
	}
	relue, err = d.LireRevision(revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if relue.Libelle == nil || *relue.Libelle != "correction d'une coquille" {
		t.Fatalf("la correction aurait dû être appliquée en base : %+v", relue)
	}

	// même immuabilité sur les composants : refus sans correction
	compID := composants[0].ID
	repComposantRefus := envoyerFormulaireModele(t, client, http.MethodPut,
		revisionURL+"/composants/"+strconv.FormatInt(compID, 10), jeton, url.Values{
			"nature": {"DISQUE_DATA"}, "code": {"ssd"},
			"quantite": {"48"}, "capacite_unitaire": {"24"}, "unite": {"TO"},
		})
	if repComposantRefus.StatusCode != http.StatusOK {
		t.Fatalf("modification composant sans correction : attendu 200, obtenu %d", repComposantRefus.StatusCode)
	}
	if composantsApres, _ := d.ListerComposants(revisionID); composantsApres[0].Quantite != 24 {
		t.Fatalf("le refus ne doit rien modifier sur le composant : %+v", composantsApres)
	}

	// ... et passe avec la case cochée
	repComposantCorrection := envoyerFormulaireModele(t, client, http.MethodPut,
		revisionURL+"/composants/"+strconv.FormatInt(compID, 10), jeton, url.Values{
			"nature": {"DISQUE_DATA"}, "code": {"ssd"},
			"quantite": {"48"}, "capacite_unitaire": {"24"}, "unite": {"TO"}, "corriger": {"1"},
		})
	if repComposantCorrection.StatusCode != http.StatusOK {
		t.Fatalf("correction composant : attendu 200, obtenu %d", repComposantCorrection.StatusCode)
	}
	if composantsApres, _ := d.ListerComposants(revisionID); len(composantsApres) != 1 || composantsApres[0].Quantite != 48 {
		t.Fatalf("la correction aurait dû s'appliquer sur le composant : %+v", composantsApres)
	}
}

// TestModeleChampsFinanciersEtFinLease vérifie le formulaire complet de la
// page de détail (financement, coûts, description — absents de la création)
// et le calcul de la date de fin de lease.
func TestModeleChampsFinanciersEtFinLease(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/modeles")

	m, err := d.CreerModele(depot.Modele{Type: "ECO", Annee: 2025, Code: "ECO-2025"})
	if err != nil {
		t.Fatal(err)
	}
	modeleURL := serveur.URL + "/modeles/" + strconv.FormatInt(m.ID, 10)

	rep := envoyerFormulaireModele(t, client, http.MethodPut, modeleURL, jeton, url.Values{
		"type": {"ECO"}, "annee": {"2025"}, "code": {"ECO-2025"},
		"description": {"Stockage dense"}, "mode_financement": {"LEASE"},
		"duree_lease_mois": {"60"}, "date_debut_lease": {"2025-04-01"},
		"prix_fournisseur_ht": {"42000"}, "cout_annuel_ht": {"9000"}, "duree_cout_annees": {"5"},
	})
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("modification des champs financiers : attendu 200, obtenu %d", rep.StatusCode)
	}
	frag := corps(t, rep)
	if !strings.Contains(frag, "2030-04-01") {
		t.Fatalf("la fin de lease calculée (2030-04-01) devrait apparaître : %s", frag)
	}

	relu, err := d.LireModele(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if relu.CoutAnnuelHT == nil || *relu.CoutAnnuelHT != 9000 {
		t.Fatalf("le coût annuel aurait dû être persisté : %+v", relu)
	}
	if !relu.Actif {
		t.Fatal("la modification des champs financiers ne doit pas désactiver le modèle")
	}

	// une année mal formée est un refus métier (200, message), pas un 500
	repInvalide := envoyerFormulaireModele(t, client, http.MethodPut, modeleURL, jeton, url.Values{
		"type": {"ECO"}, "annee": {"pas-un-nombre"}, "code": {"ECO-2025"},
	})
	if repInvalide.StatusCode != http.StatusOK {
		t.Fatalf("année invalide : attendu 200, obtenu %d", repInvalide.StatusCode)
	}
	corpsInvalide := corps(t, repInvalide)
	if !strings.Contains(corpsInvalide, "année") {
		t.Fatal("le message d'erreur sur l'année invalide devrait apparaître")
	}
}

// TestModeleNoeudsParTechno vérifie la table des nœuds installables par
// techno : une ligne par techno active même sans définition préalable
// (valeur 0), et l'écriture par DefinirNoeuds.
func TestModeleNoeudsParTechno(t *testing.T) {
	serveur, d := serveurDeTest(t)
	client := clientConnecte(t, serveur)
	jeton := jetonCSRF(t, client, serveur.URL, "/modeles")

	techno, err := d.CreerTechno(depot.Techno{Code: "ELASTIC", Libelle: "Elasticsearch"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := d.CreerModele(depot.Modele{Type: "DENSE", Annee: 2025, Code: "DENSE-2025-B"})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := d.CreerRevision(depot.Revision{ModeleID: m.ID, DateEffet: "2025-01-01"})
	if err != nil {
		t.Fatal(err)
	}

	// la révision apparaît avec une ligne nœuds à 0 pour la techno, jamais définie
	repDetail, err := client.Get(serveur.URL + "/modeles/" + strconv.FormatInt(m.ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corps(t, repDetail), "Elasticsearch") {
		t.Fatal("la techno active devrait apparaître dans la table des nœuds, même sans définition")
	}

	noeudsURL := serveur.URL + "/modeles/" + strconv.FormatInt(m.ID, 10) +
		"/revisions/" + strconv.FormatInt(rev.ID, 10) +
		"/noeuds/" + strconv.FormatInt(techno.ID, 10)

	rep := envoyerFormulaireModele(t, client, http.MethodPut, noeudsURL, jeton, url.Values{"nb_noeuds": {"6"}})
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("définition des nœuds : attendu 200, obtenu %d", rep.StatusCode)
	}
	if frag := corps(t, rep); !strings.Contains(frag, `value="6"`) {
		t.Fatalf("le nombre de nœuds enregistré devrait apparaître : %s", frag)
	}
	nb, err := d.LireNoeuds(rev.ID, techno.ID)
	if err != nil || nb != 6 {
		t.Fatalf("les nœuds auraient dû être persistés : %v / %v", nb, err)
	}
}
