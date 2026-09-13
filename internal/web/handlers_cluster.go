package web

import (
	"net/http"

	"parallax/internal/depot"
)

// routesClusters suit le patron de routesProjets (handlers_projet.go), étendu
// aux six dimensions du cluster et à un jeu de filtres au-dessus du tableau.
// Lignes à ajouter dans server.go (routes()) : voir le rapport d'intégration.
func (s *serveur) routesClusters() {
	s.mux.HandleFunc("GET /clusters", s.lecteur(s.clustersPage))
	s.mux.HandleFunc("GET /clusters/tableau", s.lecteur(s.clustersTableau))
	s.mux.HandleFunc("POST /clusters", s.editeur(s.clustersCreer))
	s.mux.HandleFunc("GET /clusters/{id}", s.lecteur(s.clustersLigne))
	s.mux.HandleFunc("GET /clusters/{id}/editer", s.editeur(s.clustersFormulaireEdition))
	s.mux.HandleFunc("PUT /clusters/{id}", s.editeur(s.clustersModifier))
	s.mux.HandleFunc("POST /clusters/{id}/archiver", s.editeur(s.clustersArchiver))
	s.mux.HandleFunc("POST /clusters/{id}/reactiver", s.editeur(s.clustersReactiver))
}

// clusterLigne incorpore depot.Cluster, un message d'erreur optionnel (patron
// projetLigne) et les libellés des dimensions — le cluster ne stocke que des
// identifiants, et gabarit ne doit jamais avoir à résoudre lui-même un id en
// libellé (ce serait de la logique métier dans le template).
type clusterLigne struct {
	depot.Cluster
	Erreur               string
	ProjetLibelle        string
	EnvironnementLibelle string
	TechnoLibelle        string
	TierLibelle          string // vide si le cluster n'a pas de tier
	UsageLibelle         string // vide si le cluster n'a pas d'usage
}

// TierIDOuZero et UsageIDOuZero exposent les dimensions optionnelles comme un
// int64 (0 = absent) plutôt qu'un *int64, pour que le gabarit d'édition
// compare directement la valeur à l'id d'une <option> avec la fonction eq
// intégrée — html/template ne sait pas comparer un *int64 à un int64, et
// gabarits.go (hors périmètre de cet écran) n'expose pas de fonction de
// déréférencement générique.
func (l clusterLigne) TierIDOuZero() int64 {
	if l.TierID == nil {
		return 0
	}
	return *l.TierID
}

func (l clusterLigne) UsageIDOuZero() int64 {
	if l.UsageFonctionnelID == nil {
		return 0
	}
	return *l.UsageFonctionnelID
}

// referentielsCluster porte les listes nécessaires aux <select> des
// formulaires de création et d'édition (dimensions actives seulement, comme
// les autres écrans de référentiel).
type referentielsCluster struct {
	Projets        []depot.Projet
	Environnements []depot.Environnement
	Technos        []depot.Techno
	Tiers          []depot.Tier
	Usages         []depot.UsageFonctionnel
}

// clusterEdition est le contexte de "clusters_ligne_edition" : la ligne (avec
// ses libellés) plus les référentiels nécessaires pour repeupler ses propres
// <select> — à la différence de projets_ligne_edition (deux champs texte),
// une ligne de cluster en édition doit reconstruire six sélecteurs sans
// recharger toute la page.
type clusterEdition struct {
	clusterLigne
	Referentiels referentielsCluster
}

// libellesCluster associe l'id de chaque dimension à son libellé, calculé une
// fois par requête puis appliqué à toutes les lignes affichées — évite une
// requête de résolution par ligne et par dimension.
type libellesCluster struct {
	Projets        map[int64]string
	Environnements map[int64]string
	Technos        map[int64]string
	Tiers          map[int64]string
	Usages         map[int64]string
}

func (s *serveur) chargerReferentielsCluster() (referentielsCluster, error) {
	projets, err := s.depot.ListerProjets(false)
	if err != nil {
		return referentielsCluster{}, err
	}
	environnements, err := s.depot.ListerEnvironnements()
	if err != nil {
		return referentielsCluster{}, err
	}
	technos, err := s.depot.ListerTechnos(false)
	if err != nil {
		return referentielsCluster{}, err
	}
	tiers, err := s.depot.ListerTiers()
	if err != nil {
		return referentielsCluster{}, err
	}
	usages, err := s.depot.ListerUsages()
	if err != nil {
		return referentielsCluster{}, err
	}
	return referentielsCluster{
		Projets: projets, Environnements: environnements,
		Technos: technos, Tiers: tiers, Usages: usages,
	}, nil
}

// chargerLibellesCluster inclut les référentiels archivés (inclureInactifs :
// true) : un cluster créé avant l'archivage de sa techno, par exemple, doit
// quand même afficher un libellé plutôt qu'une case vide.
func (s *serveur) chargerLibellesCluster() (libellesCluster, error) {
	projets, err := s.depot.ListerProjets(true)
	if err != nil {
		return libellesCluster{}, err
	}
	environnements, err := s.depot.ListerEnvironnements()
	if err != nil {
		return libellesCluster{}, err
	}
	technos, err := s.depot.ListerTechnos(true)
	if err != nil {
		return libellesCluster{}, err
	}
	tiers, err := s.depot.ListerTiers()
	if err != nil {
		return libellesCluster{}, err
	}
	usages, err := s.depot.ListerUsages()
	if err != nil {
		return libellesCluster{}, err
	}

	lc := libellesCluster{
		Projets:        make(map[int64]string, len(projets)),
		Environnements: make(map[int64]string, len(environnements)),
		Technos:        make(map[int64]string, len(technos)),
		Tiers:          make(map[int64]string, len(tiers)),
		Usages:         make(map[int64]string, len(usages)),
	}
	for _, p := range projets {
		lc.Projets[p.ID] = p.Libelle
	}
	for _, e := range environnements {
		lc.Environnements[e.ID] = e.Libelle
	}
	for _, t := range technos {
		lc.Technos[t.ID] = t.Libelle
	}
	for _, t := range tiers {
		lc.Tiers[t.ID] = t.Libelle
	}
	for _, u := range usages {
		lc.Usages[u.ID] = u.Libelle
	}
	return lc, nil
}

func clusterEnLigne(c depot.Cluster, lc libellesCluster) clusterLigne {
	l := clusterLigne{
		Cluster:              c,
		ProjetLibelle:        lc.Projets[c.ProjetID],
		EnvironnementLibelle: lc.Environnements[c.EnvironnementID],
		TechnoLibelle:        lc.Technos[c.TechnoID],
	}
	if c.TierID != nil {
		l.TierLibelle = lc.Tiers[*c.TierID]
	}
	if c.UsageFonctionnelID != nil {
		l.UsageLibelle = lc.Usages[*c.UsageFonctionnelID]
	}
	return l
}

func clustersEnLignes(clusters []depot.Cluster, lc libellesCluster) []clusterLigne {
	out := make([]clusterLigne, len(clusters))
	for i, c := range clusters {
		out[i] = clusterEnLigne(c, lc)
	}
	return out
}

// clusterFiltreDepuisRequete lit les cinq filtres et la case « inclure les
// archivés ». Noms de champs préfixés filtre_ : le formulaire de création
// partage la page avec ces filtres et porte ses propres champs projet_id,
// environnement_id, etc. — des noms identiques provoqueraient une collision
// lorsque la création inclut l'état courant des filtres (hx-include) pour
// réafficher un tableau qui les respecte toujours.
func clusterFiltreDepuisRequete(r *http.Request) (depot.FiltreCluster, error) {
	var f depot.FiltreCluster
	var err error
	if f.ProjetID, err = idOptionnel(r, "filtre_projet_id"); err != nil {
		return f, err
	}
	if f.EnvironnementID, err = idOptionnel(r, "filtre_environnement_id"); err != nil {
		return f, err
	}
	if f.TechnoID, err = idOptionnel(r, "filtre_techno_id"); err != nil {
		return f, err
	}
	if f.TierID, err = idOptionnel(r, "filtre_tier_id"); err != nil {
		return f, err
	}
	if f.UsageID, err = idOptionnel(r, "filtre_usage_id"); err != nil {
		return f, err
	}
	f.InclureInactifs = inclureInactifs(r)
	return f, nil
}

// parametresFiltreCluster : les paramètres de requête que les liens d'export
// du tableau des clusters reprennent — les cinq filtres et la case « inclure
// les archivés », exactement ce que clusterFiltreDepuisRequete lit.
var parametresFiltreCluster = []string{
	"filtre_projet_id", "filtre_environnement_id", "filtre_techno_id",
	"filtre_tier_id", "filtre_usage_id", "inclure_inactifs",
}

// exportClusters : liens d'export posés dans le fragment "clusters_tableau",
// vers la page /clusters avec les filtres courants (voir exportProjets,
// handlers_projet.go) — c'est clustersPage qui répond.
func exportClusters(r *http.Request) exportLiens {
	return liensExportVers(r, "/clusters", "", parametresFiltreCluster...)
}

// tableauClusters est la forme exportable du tableau : les libellés des
// dimensions, jamais leurs identifiants.
func tableauClusters(lignes []clusterLigne) tableau {
	t := tableau{Titre: "Clusters", Colonnes: []string{"Nom", "Projet", "Environnement", "Techno", "Tier", "Usage", "Actif"}}
	for _, l := range lignes {
		t.Lignes = append(t.Lignes, []any{l.Nom, l.ProjetLibelle, l.EnvironnementLibelle, l.TechnoLibelle, l.TierLibelle, l.UsageLibelle, l.Actif})
	}
	return t
}

func (s *serveur) clustersPage(w http.ResponseWriter, r *http.Request) {
	// export universel (tableau.go) : les filtres que le fragment a mis dans
	// les liens s'appliquent ici. La page HTML, elle, reste affichée sans
	// filtre comme avant : ses <select> ne reflètent pas l'URL, et un tableau
	// filtré sous des sélecteurs à « Tous » serait trompeur.
	if s.exporter(w, r, func() (tableau, error) {
		f, err := clusterFiltreDepuisRequete(r)
		if err != nil {
			return tableau{}, err
		}
		clusters, err := s.depot.ListerClusters(f)
		if err != nil {
			return tableau{}, err
		}
		lc, err := s.chargerLibellesCluster()
		if err != nil {
			return tableau{}, err
		}
		return tableauClusters(clustersEnLignes(clusters, lc)), nil
	}) {
		return
	}
	ref, err := s.chargerReferentielsCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.clusters"), "clusters_page", map[string]any{
		"Clusters":       clustersEnLignes(clusters, lc),
		"Projets":        ref.Projets,
		"Environnements": ref.Environnements,
		"Technos":        ref.Technos,
		"Tiers":          ref.Tiers,
		"Usages":         ref.Usages,
		"Export":         liensExportDepuis("/clusters", nil, ""),
	})
}

// rendreClustersTableau factorise la relecture filtrée utilisée par
// clustersTableau (filtres) et clustersCreer (retourne toujours le tableau
// entier, jamais seulement la ligne créée — règle n°2 du patron).
func (s *serveur) rendreClustersTableau(w http.ResponseWriter, r *http.Request, f depot.FiltreCluster, erreur string) {
	clusters, err := s.depot.ListerClusters(f)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "clusters_tableau", map[string]any{
		"Clusters": clustersEnLignes(clusters, lc),
		"Erreur":   erreur,
		"Export":   exportClusters(r),
	})
}

func (s *serveur) clustersTableau(w http.ResponseWriter, r *http.Request) {
	f, err := clusterFiltreDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rendreClustersTableau(w, r, f, "")
}

func (s *serveur) clustersCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	tierID, err := idOptionnel(r, "tier_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	usageID, err := idOptionnel(r, "usage_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, errCreation := s.depotPour(r).CreerCluster(depot.Cluster{
		Nom:                r.FormValue("nom"),
		ProjetID:           idRequis(r, "projet_id"),
		EnvironnementID:    idRequis(r, "environnement_id"),
		TechnoID:           idRequis(r, "techno_id"),
		TierID:             tierID,
		UsageFonctionnelID: usageID,
	})
	if errCreation != nil && !erreurMetier(errCreation) {
		s.erreurServeur(w, r, errCreation)
		return
	}

	f, err := clusterFiltreDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rendreClustersTableau(w, r, f, messageUtilisateur(errCreation))
}

func (s *serveur) clustersLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.depot.LireCluster(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "clusters_ligne", clusterEnLigne(c, lc))
}

func (s *serveur) clustersFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c, err := s.depot.LireCluster(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	ref, err := s.chargerReferentielsCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "clusters_ligne_edition", clusterEdition{
		clusterLigne: clusterEnLigne(c, lc), Referentiels: ref,
	})
}

// clustersModifier lit le nom, les six dimensions et le commentaire (porté
// par un champ caché du gabarit d'édition, qui n'expose pas de zone de texte
// dédiée — voir le commentaire de tête de clusters/liste.html) : ainsi
// ModifierCluster, qui écrase la colonne commentaire, ne perd jamais une
// valeur existante faute d'un champ visible pour la ressaisir.
func (s *serveur) clustersModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	tierID, err := idOptionnel(r, "tier_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	usageID, err := idOptionnel(r, "usage_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	c := depot.Cluster{
		ID:                 id,
		Nom:                r.FormValue("nom"),
		ProjetID:           idRequis(r, "projet_id"),
		EnvironnementID:    idRequis(r, "environnement_id"),
		TechnoID:           idRequis(r, "techno_id"),
		TierID:             tierID,
		UsageFonctionnelID: usageID,
		Commentaire:        texteOptionnel(r, "commentaire"),
	}
	if err := s.depotPour(r).ModifierCluster(c); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		// ModifierCluster ne touche pas actif : le relire évite d'afficher un
		// cluster "archivé" à tort (c.Actif serait la valeur zéro Go), comme
		// pour projetsModifier.
		actuel, errLecture := s.depot.LireCluster(id)
		if errLecture == nil {
			c.Actif = actuel.Actif
		}
		lc, errLib := s.chargerLibellesCluster()
		if errLib != nil {
			s.erreurServeur(w, r, errLib)
			return
		}
		ref, errRef := s.chargerReferentielsCluster()
		if errRef != nil {
			s.erreurServeur(w, r, errRef)
			return
		}
		ligne := clusterEnLigne(c, lc)
		ligne.Erreur = messageUtilisateur(err)
		s.rendreFragment(w, r, "clusters_ligne_edition", clusterEdition{clusterLigne: ligne, Referentiels: ref})
		return
	}

	relu, err := s.depot.LireCluster(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "clusters_ligne", clusterEnLigne(relu, lc))
}

func (s *serveur) clustersArchiver(w http.ResponseWriter, r *http.Request) {
	s.clustersBasculerActivite(w, r, s.depotPour(r).ArchiverCluster)
}

func (s *serveur) clustersReactiver(w http.ResponseWriter, r *http.Request) {
	s.clustersBasculerActivite(w, r, s.depotPour(r).ReactiverCluster)
}

func (s *serveur) clustersBasculerActivite(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := action(id); err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	c, err := s.depot.LireCluster(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "clusters_ligne", clusterEnLigne(c, lc))
}
