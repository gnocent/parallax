package web

import (
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"parallax/internal/auth"
	"parallax/internal/db"
	"parallax/internal/depot"
)

// serveurImportModelesDeTest est l'équivalent de serveurDeTest (web_test.go)
// pour cet écran, mais construit son propre *serveur au lieu de passer par
// Nouveau (server.go) : cet écran n'a volontairement pas encore d'appel
// s.routesImportModeles() dans (*serveur).routes() (server.go n'est jamais
// modifié par cette tâche — voir le commentaire de routesImportModeles).
// Le jour où l'intégration ajoute cet appel dans routes(), ce test continuera
// de passer inchangé : il exerce exactement la même fonction de câblage.
func serveurImportModelesDeTest(t *testing.T) (*httptest.Server, *depot.Depot) {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "web-test-import-modeles.db")
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
	// seules les routes communes nécessaires à l'authentification (voir
	// server.go routes()) plus l'écran testé ici.
	s.mux.HandleFunc("GET /connexion", s.connexionFormulaire)
	s.mux.HandleFunc("POST /connexion", s.connexionSoumettre)
	s.mux.HandleFunc("POST /deconnexion", s.exigerAuth(s.verifierCSRF(s.deconnexionSoumettre)))
	s.routesImportModeles()

	handler := recuperer(journaliser(s.chargerSession(s.mux)))
	serveurHTTP := httptest.NewServer(handler)
	t.Cleanup(serveurHTTP.Close)
	return serveurHTTP, d
}

// jetonCSRFDepuisPage lit le jeton CSRF affiché par hx-headers sur <body> —
// même extraction que TestParcoursProjetCompletParHTTP (web_test.go), sur une
// page quelconque de l'écran testé.
func jetonCSRFDepuisPage(t *testing.T, client *http.Client, serveur *httptest.Server, chemin string) string {
	t.Helper()
	rep, err := client.Get(serveur.URL + chemin)
	if err != nil {
		t.Fatal(err)
	}
	page := corps(t, rep)
	const marqueur = `X-CSRF-Token":"`
	i := strings.Index(page, marqueur)
	if i < 0 {
		t.Fatal("jeton CSRF absent de la page")
	}
	i += len(marqueur)
	jeton := page[i : strings.Index(page[i:], `"`)+i]
	if jeton == "" {
		t.Fatal("jeton CSRF vide")
	}
	return jeton
}

// posterCSVModeles envoie un fichier CSV en multipart/form-data vers
// /import/modeles/analyser, comme le formulaire <input type="file"> de
// l'écran.
func posterCSVModeles(t *testing.T, client *http.Client, serveur *httptest.Server, jetonCSRF, contenu string) *http.Response {
	t.Helper()
	var corpsMultipart strings.Builder
	ecrivain := multipart.NewWriter(&corpsMultipart)
	part, err := ecrivain.CreateFormFile("fichier", "modeles.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(contenu)); err != nil {
		t.Fatal(err)
	}
	if err := ecrivain.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, serveur.URL+"/import/modeles/analyser", strings.NewReader(corpsMultipart.String()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ecrivain.FormDataContentType())
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// extraireJetonImport lit la valeur de l'input caché "jeton" du formulaire de
// confirmation, présent dans le fragment d'une analyse réussie.
func extraireJetonImport(t *testing.T, fragment string) string {
	t.Helper()
	const marqueur = `name="jeton" value="`
	i := strings.Index(fragment, marqueur)
	if i < 0 {
		t.Fatalf("jeton d'import absent du fragment : %s", fragment)
	}
	i += len(marqueur)
	fin := strings.Index(fragment[i:], `"`)
	if fin < 0 {
		t.Fatalf("jeton d'import mal formé dans le fragment : %s", fragment)
	}
	return fragment[i : i+fin]
}

func confirmerImportModeles(t *testing.T, client *http.Client, serveur *httptest.Server, jetonCSRF, jetonImport string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, serveur.URL+"/import/modeles/confirmer",
		strings.NewReader(url.Values{"jeton": {jetonImport}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", jetonCSRF)
	rep, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

// TestImportModelesValideCreeTout couvre le parcours complet : analyse (zéro
// écriture), confirmation, puis vérification en base du modèle, de sa
// révision unique et de ses composants.
func TestImportModelesValideCreeTout(t *testing.T) {
	serveur, d := serveurImportModelesDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/modeles")

	csv := "type;annee;code;mode_financement;cpu_coeurs;ram_go;ssd_nb;ssd_to;gpu_nb;gpu_ram_go\n" +
		"DENSE;2025;DENSE-2025;LEASE;64;1024;24;7,68;2;80\n" +
		"LEGACY;2024;LEGACY-2024;ACHAT;;;;;;\n"

	repAnalyse := posterCSVModeles(t, client, serveur, jetonCSRF, csv)
	fragmentAnalyse := corps(t, repAnalyse)
	if repAnalyse.StatusCode != http.StatusOK {
		t.Fatalf("analyse : attendu 200, obtenu %d : %s", repAnalyse.StatusCode, fragmentAnalyse)
	}
	if !strings.Contains(fragmentAnalyse, "2 modèle(s) prêt(s)") {
		t.Fatalf("le résumé de simulation devrait annoncer 2 modèles prêts : %s", fragmentAnalyse)
	}

	// zéro écriture après l'analyse.
	modelesAvant, err := d.ListerModeles(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(modelesAvant) != 0 {
		t.Fatalf("l'analyse ne doit rien écrire, trouvé %d modèle(s)", len(modelesAvant))
	}

	jetonImport := extraireJetonImport(t, fragmentAnalyse)
	repConfirme := confirmerImportModeles(t, client, serveur, jetonCSRF, jetonImport)
	fragmentConfirme := corps(t, repConfirme)
	if repConfirme.StatusCode != http.StatusOK {
		t.Fatalf("confirmation : attendu 200, obtenu %d : %s", repConfirme.StatusCode, fragmentConfirme)
	}
	if !strings.Contains(fragmentConfirme, "2 modèle(s) importé(s)") {
		t.Fatalf("le résumé final devrait annoncer 2 modèles importés : %s", fragmentConfirme)
	}

	// le modèle avec composants porte bien sa révision et ses composants.
	avecComposants, err := d.LireModeleParCode("DENSE-2025")
	if err != nil {
		t.Fatalf("le modèle DENSE-2025 aurait dû être créé : %v", err)
	}
	revisions, err := d.ListerRevisions(avecComposants.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 1 || revisions[0].Numero != 1 {
		t.Fatalf("attendu une révision numéro 1, obtenu %+v", revisions)
	}
	composants, err := d.ListerComposants(revisions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(composants) != 5 {
		t.Fatalf("attendu 5 composants (cpu, ram, ssd, gpu, gpu_ram), obtenu %d : %+v", len(composants), composants)
	}
	attendus := map[string][4]any{
		"cpu":     {"CPU", "CORE", 1.0, 64.0},
		"ram":     {"RAM", "GO", 1.0, 1024.0},
		"ssd":     {"DISQUE_DATA", "TO", 24.0, 7.68},
		"gpu":     {"GPU", "UNITE", 2.0, 1.0},
		"gpu_ram": {"GPU", "GO", 2.0, 80.0},
	}
	for _, c := range composants {
		a, ok := attendus[c.Code]
		if !ok {
			t.Fatalf("composant inattendu : %+v", c)
		}
		if c.Nature != a[0] || c.Unite != a[1] || c.Quantite != a[2] || c.CapaciteUnitaire != a[3] {
			t.Fatalf("composant %s : attendu %v, obtenu %+v", c.Code, a, c)
		}
		delete(attendus, c.Code)
	}
	if len(attendus) != 0 {
		t.Fatalf("composants manquants : %v", attendus)
	}

	// le modèle sans triplet composant ne porte aucune révision.
	sansComposants, err := d.LireModeleParCode("LEGACY-2024")
	if err != nil {
		t.Fatalf("le modèle LEGACY-2024 aurait dû être créé : %v", err)
	}
	revisionsVides, err := d.ListerRevisions(sansComposants.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisionsVides) != 0 {
		t.Fatalf("un modèle sans composant ne doit porter aucune révision, obtenu %+v", revisionsVides)
	}

	// le jeton d'import ne sert qu'une fois.
	repRejoue := confirmerImportModeles(t, client, serveur, jetonCSRF, jetonImport)
	if !strings.Contains(corps(t, repRejoue), "expiré") {
		t.Fatal("un jeton déjà consommé devrait être signalé comme expiré/introuvable")
	}
}

// TestImportModelesLigneInvalideNAffecteRienEnBase est le test clé de
// l'acceptation v1.4 : une seule ligne invalide dans un fichier de plusieurs
// lignes valides ne doit laisser aucune trace en base, pas même les lignes
// par ailleurs correctes.
func TestImportModelesLigneInvalideNAffecteRienEnBase(t *testing.T) {
	serveur, d := serveurImportModelesDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/modeles")

	avant, err := d.ListerModeles(true)
	if err != nil {
		t.Fatal(err)
	}
	nbAvant := len(avant)

	// deuxième ligne : année non numérique.
	csv := "type;annee;code\n" +
		"DENSE;2025;DENSE-2025\n" +
		"LEGACY;pas-une-annee;LEGACY-2024\n"

	repAnalyse := posterCSVModeles(t, client, serveur, jetonCSRF, csv)
	fragmentAnalyse := corps(t, repAnalyse)
	if !strings.Contains(fragmentAnalyse, "Ligne 3") {
		t.Fatalf("l'erreur devrait pointer la ligne 3 : %s", fragmentAnalyse)
	}
	// "Confirmer" seul ne suffit plus à détecter le bouton depuis l'ajout de
	// l'indicateur d'étapes (import_etapes.html), qui nomme aussi l'étape à
	// venir dans un rapport en erreur — on cherche l'action elle-même.
	if strings.Contains(fragmentAnalyse, "/import/modeles/confirmer") {
		t.Fatal("un rapport en erreur ne doit pas proposer de bouton de confirmation")
	}

	apresAnalyse, err := d.ListerModeles(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(apresAnalyse) != nbAvant {
		t.Fatalf("l'analyse en erreur ne doit rien écrire : avant %d, après %d", nbAvant, len(apresAnalyse))
	}

	// paire incomplète : ssd_nb renseigné, capacité unitaire absente.
	csvComposantIncomplet := "type;annee;code;ssd_nb;ssd_to\n" +
		"DENSE;2025;DENSE-2025;24;\n"
	repAnalyse2 := posterCSVModeles(t, client, serveur, jetonCSRF, csvComposantIncomplet)
	fragment2 := corps(t, repAnalyse2)
	if !strings.Contains(fragment2, "Ligne 2") || !strings.Contains(fragment2, "ssd_nb et ssd_to") {
		t.Fatalf("l'erreur de paire incomplète devrait pointer la ligne 2 et nommer les colonnes : %s", fragment2)
	}

	// attribut GPU sans nombre de GPU : la quantité partagée manque.
	csvGPUSansNombre := "type;annee;code;gpu_ram_go\n" +
		"DENSE;2025;DENSE-2025;80\n"
	fragment3 := corps(t, posterCSVModeles(t, client, serveur, jetonCSRF, csvGPUSansNombre))
	if !strings.Contains(fragment3, "Ligne 2") || !strings.Contains(fragment3, "gpu_nb et gpu_ram_go") {
		t.Fatalf("un attribut GPU sans gpu_nb devrait être refusé en nommant les colonnes : %s", fragment3)
	}

	apresAnalyse2, err := d.ListerModeles(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(apresAnalyse2) != nbAvant {
		t.Fatalf("un triplet composant incomplet ne doit rien écrire : avant %d, après %d", nbAvant, len(apresAnalyse2))
	}
}

// TestImportModelesCodeExistantEnBase vérifie qu'un code déjà présent en base
// (créé hors import) rejette la ligne avec un message explicite.
func TestImportModelesCodeExistantEnBase(t *testing.T) {
	serveur, d := serveurImportModelesDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/modeles")

	if _, err := d.CreerModele(depot.Modele{Type: "DENSE", Annee: 2025, Code: "DENSE-2025"}); err != nil {
		t.Fatal(err)
	}

	csv := "type;annee;code\nDENSE;2025;DENSE-2025\n"
	rep := posterCSVModeles(t, client, serveur, jetonCSRF, csv)
	fragment := corps(t, rep)
	if !strings.Contains(fragment, "Ligne 2") || !strings.Contains(fragment, "déjà utilisé") {
		t.Fatalf("le code déjà existant en base devrait être signalé explicitement : %s", fragment)
	}
}

// TestImportModelesCodeDuplique vérifie qu'un code répété dans le même
// fichier rejette les deux lignes, avec les deux numéros de ligne dans le
// message.
func TestImportModelesCodeDuplique(t *testing.T) {
	serveur, _ := serveurImportModelesDeTest(t)
	client := clientConnecte(t, serveur)
	jetonCSRF := jetonCSRFDepuisPage(t, client, serveur, "/import/modeles")

	csv := "type;annee;code\n" +
		"DENSE;2025;DOUBLON\n" +
		"LEGACY;2024;DOUBLON\n"

	rep := posterCSVModeles(t, client, serveur, jetonCSRF, csv)
	fragment := corps(t, rep)
	if !strings.Contains(fragment, "Ligne 2") || !strings.Contains(fragment, "Ligne 3") {
		t.Fatalf("les deux lignes en conflit devraient apparaître : %s", fragment)
	}
	if !strings.Contains(fragment, "dupliqué") {
		t.Fatalf("le message devrait mentionner le doublon : %s", fragment)
	}
}
