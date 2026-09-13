package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"parallax/internal/auth"
	"parallax/internal/depot"
)

// routesDemandes enregistre l'écran de génération des demandes de matériel
// (backlog v3.2) : la liste filtrable des serveurs avec leur bloc de
// description rendu par le gabarit global, l'édition en ligne du numéro de
// fiche, et l'affectation en masse d'un numéro de demande. À câbler dans
// server.go (routes()) par l'intégration ; jamais depuis ce fichier.
//
//	GET  /demandes             page (filtres, tableau, blocs générés, export)
//	PUT  /demandes/{id}/fiche  numéro de fiche d'un serveur, répond la ligne
//	                            avec son bloc recalculé (fragment htmx)
//	POST /demandes/numero      numéro de demande sur les serveurs listés
//	                            (formulaire classique, redirige vers la page)
func (s *serveur) routesDemandes() {
	s.mux.HandleFunc("GET /demandes", s.lecteur(s.demandesPage))
	s.mux.HandleFunc("PUT /demandes/{id}/fiche", s.editeur(s.demandeFicheModifier))
	s.mux.HandleFunc("POST /demandes/numero", s.editeur(s.demandesNumeroMasse))
}

// statutsFiltreDemande : les quatre statuts, HYPOTHESE compris — une demande
// se prépare le plus souvent sur des serveurs hypothétiques.
var statutsFiltreDemande = []string{
	depot.StatutHypothese, depot.StatutCommande, depot.StatutEnService, depot.StatutDecommissionne,
}

// filtreDemande est l'état de la barre de filtres tel que le gabarit le
// relit (valeurs zéro = pas de filtre, comme ScenarioActifID) et tel que le
// formulaire de numéro en masse le rejoue en champs cachés pour revenir sur
// la même page. Les noms de paramètres sont ceux de la requête.
type filtreDemande struct {
	ScenarioID      int64  // scenario
	ProjetID        int64  // projet
	EnvironnementID int64  // environnement
	ClusterID       int64  // cluster
	Statut          string // statut
	Demande         string // demande (numéro de demande exact)
	SansNumero      bool   // sans_numero
}

func filtreDemandeDepuisRequete(r *http.Request) (filtreDemande, error) {
	var f filtreDemande
	if id := scenarioDepuisRequete(r); id != nil {
		f.ScenarioID = *id
	}
	for _, champ := range []struct {
		nom   string
		cible *int64
	}{
		{"projet", &f.ProjetID}, {"environnement", &f.EnvironnementID}, {"cluster", &f.ClusterID},
	} {
		id, err := idOptionnel(r, champ.nom)
		if err != nil {
			return f, err
		}
		if id != nil {
			*champ.cible = *id
		}
	}
	f.Statut = strings.TrimSpace(r.FormValue("statut"))
	f.Demande = strings.TrimSpace(r.FormValue("demande"))
	f.SansNumero = r.FormValue("sans_numero") != ""
	return f, nil
}

// versDepot traduit l'état d'écran en filtre de dépôt (pointeurs nil pour
// « pas de filtre »).
func (f filtreDemande) versDepot() depot.FiltreDemande {
	var fd depot.FiltreDemande
	ptr := func(v int64) *int64 {
		if v == 0 {
			return nil
		}
		return &v
	}
	fd.ScenarioID = ptr(f.ScenarioID)
	fd.ProjetID = ptr(f.ProjetID)
	fd.EnvironnementID = ptr(f.EnvironnementID)
	fd.ClusterID = ptr(f.ClusterID)
	if f.Statut != "" {
		fd.Statut = &f.Statut
	}
	if f.Demande != "" {
		fd.DemandeRef = &f.Demande
	}
	fd.SansNumero = f.SansNumero
	return fd
}

// valeursURL rejoue le filtre en paramètres de requête — c'est ce que le
// formulaire de numéro en masse renvoie après action, pour réafficher la
// même sélection.
func (f filtreDemande) valeursURL() url.Values {
	q := url.Values{}
	poser := func(nom string, v int64) {
		if v != 0 {
			q.Set(nom, strconv.FormatInt(v, 10))
		}
	}
	poser("scenario", f.ScenarioID)
	poser("projet", f.ProjetID)
	poser("environnement", f.EnvironnementID)
	poser("cluster", f.ClusterID)
	if f.Statut != "" {
		q.Set("statut", f.Statut)
	}
	if f.Demande != "" {
		q.Set("demande", f.Demande)
	}
	if f.SansNumero {
		q.Set("sans_numero", "1")
	}
	return q
}

// demandeLigne est une fiche prête à l'affichage : le bloc rendu par le
// gabarit, et le droit d'éditer le numéro de fiche (lecteur : lecture
// seule). Erreur suit le patron projetLigne pour une réponse 200 avec
// message métier inline.
type demandeLigne struct {
	depot.FicheDemande
	Bloc       string
	PeutEditer bool
	Erreur     string
}

// clusterOption est une entrée du sélecteur de cluster, libellée avec son
// projet et son environnement : deux clusters de projets différents peuvent
// porter le même nom.
type clusterOption struct {
	ID      int64
	Libelle string
}

// chargerGabaritDemande lit et compile le gabarit global. present vaut faux
// si aucun gabarit n'est défini ; g est alors nil. Un gabarit stocké qui ne
// compile plus (catalogue restreint après coup) est traité comme absent,
// avec le message d'erreur pour l'annoncer.
func (s *serveur) chargerGabaritDemande() (g *Gabarit, present bool, probleme string, err error) {
	texte, present, err := s.depot.LireParametre(depot.ParametreGabaritDemande)
	if err != nil || !present {
		return nil, false, "", err
	}
	g, errCompil := CompilerGabarit(texte)
	if errCompil != nil {
		return nil, false, "Le gabarit enregistré n'est plus valide : " + messageUtilisateur(errCompil), nil
	}
	return g, true, "", nil
}

func rendreBloc(g *Gabarit, f depot.FicheDemande) string {
	if g == nil {
		return ""
	}
	return g.Rendre(f.Valeurs())
}

func peutEditer(r *http.Request) bool {
	identite, ok := identiteDepuis(r.Context())
	return ok && (identite.Role == depot.RoleEditeur || identite.Role == depot.RoleAdmin)
}

func (s *serveur) demandesPage(w http.ResponseWriter, r *http.Request) {
	filtre, err := filtreDemandeDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fiches, err := s.depot.ListerFichesDemande(filtre.versDepot())
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	gabarit, gabaritPresent, probleme, err := s.chargerGabaritDemande()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	editable := peutEditer(r)
	lignes := make([]demandeLigne, len(fiches))
	for i, f := range fiches {
		lignes[i] = demandeLigne{FicheDemande: f, Bloc: rendreBloc(gabarit, f), PeutEditer: editable}
	}

	// export universel (tableau.go) : mêmes données, mêmes filtres que la
	// page ; la description est le bloc tel qu'il sera collé.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Demandes de matériel", Colonnes: []string{
			"Serveur", "Cluster", "Statut", "Numéro de demande", "Numéro de fiche", "Description"}}
		for _, l := range lignes {
			t.Lignes = append(t.Lignes, []any{
				l.NomPhysique, l.Cluster, l.Statut, l.NumDemande, l.NumFiche, l.Bloc})
		}
		return t, nil
	}) {
		return
	}

	selecteur, err := s.chargerSelecteurScenarios(r)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	projets, err := s.depot.ListerProjets(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	environnements, err := s.depot.ListerEnvironnements()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	clusters, err := s.optionsClustersDemande()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	var succes string
	if n, err := strconv.Atoi(r.URL.Query().Get("affectes")); err == nil && n >= 0 {
		succes = "Numéro de demande affecté à " + strconv.Itoa(n) + " serveur(s)."
	}
	csrf := ""
	if session, ok := sessionDepuis(r.Context()); ok {
		csrf = session.CSRFToken
	}
	s.rendrePage(w, r, s.titre(r, "titre.demandes"), "demandes_page", map[string]any{
		"Lignes":          lignes,
		"Filtre":          filtre,
		"Scenarios":       selecteur.Scenarios,
		"Projets":         projets,
		"Environnements":  environnements,
		"Clusters":        clusters,
		"Statuts":         statutsFiltreDemande,
		"GabaritPresent":  gabaritPresent,
		"GabaritProbleme": probleme,
		"PeutEditer":      editable,
		"Succes":          succes,
		"CSRFToken":       csrf,
		"Export":          liensExport(r),
	})
}

// optionsClustersDemande liste les clusters actifs, libellés « nom
// (PROJET / ENV) », triés comme le dépôt les renvoie.
func (s *serveur) optionsClustersDemande() ([]clusterOption, error) {
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		return nil, err
	}
	projets, err := s.depot.ListerProjets(true)
	if err != nil {
		return nil, err
	}
	environnements, err := s.depot.ListerEnvironnements()
	if err != nil {
		return nil, err
	}
	codesProjet := make(map[int64]string, len(projets))
	for _, p := range projets {
		codesProjet[p.ID] = p.Code
	}
	codesEnv := make(map[int64]string, len(environnements))
	for _, e := range environnements {
		codesEnv[e.ID] = e.Code
	}
	out := make([]clusterOption, len(clusters))
	for i, c := range clusters {
		out[i] = clusterOption{
			ID:      c.ID,
			Libelle: c.Nom + " (" + codesProjet[c.ProjetID] + " / " + codesEnv[c.EnvironnementID] + ")",
		}
	}
	return out, nil
}

// demandeFicheModifier pose le numéro de fiche saisi dans la ligne et
// renvoie la ligne avec son bloc recalculé — c'est le « recalcul immédiat »
// du backlog. Une erreur métier (serveur disparu) revient en 200 avec le
// message dans la ligne, jamais en 4xx qu'htmx n'afficherait pas.
func (s *serveur) demandeFicheModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	errMetier := s.depotPour(r).DefinirDemandeServeurRef(id, texteOptionnel(r, "fiche"))
	if errMetier != nil && !erreurMetier(errMetier) {
		s.erreurServeur(w, r, errMetier)
		return
	}
	fiche, err := s.depot.LireFicheDemande(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	gabarit, _, _, err := s.chargerGabaritDemande()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "demandes_ligne", demandeLigne{
		FicheDemande: fiche, Bloc: rendreBloc(gabarit, fiche),
		PeutEditer: true, Erreur: messageUtilisateur(errMetier),
	})
}

// demandesNumeroMasse affecte le numéro saisi à tous les serveurs dont
// l'identifiant a été envoyé en champ caché — exactement ce que l'écran
// affichait, plutôt qu'un rejeu des filtres qui pourrait embarquer un
// serveur arrivé entretemps. Le numéro reste porté par le serveur
// (demande_ref), jamais par le scénario. Formulaire classique : on redirige
// vers la page avec les mêmes filtres et le nombre de serveurs touchés.
func (s *serveur) demandesNumeroMasse(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	filtre, err := filtreDemandeDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, v := range r.Form["id"] {
		id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			http.Error(w, "identifiant de serveur invalide : « "+v+" »", http.StatusBadRequest)
			return
		}
		ids = append(ids, id)
	}
	n, err := s.depotPour(r).DefinirDemandeRef(ids, texteOptionnel(r, "numero"))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	q := filtre.valeursURL()
	q.Set("affectes", strconv.Itoa(n))
	http.Redirect(w, r, "/demandes?"+q.Encode(), http.StatusSeeOther)
}

// identiteOuNil renvoie l'identité de la requête, ou nil hors session — pour
// les écritures qui tracent leur auteur (modifie_par).
func identiteOuNil(r *http.Request) *auth.Identite {
	identite, ok := identiteDepuis(r.Context())
	if !ok {
		return nil
	}
	return &identite
}
