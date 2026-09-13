package web

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"parallax/internal/capacity"
	"parallax/internal/depot"
)

// routesRegles enregistre l'écran de liste des règles (création, bascule
// actif/inactif, duplication) puis la page de détail d'une règle, qui porte
// le formulaire complet d'édition — dix champs sont trop pour une ligne de
// tableau, le choix retenu est donc un formulaire complet façon détail
// modèle (voir handlers_modele.go, modeleChampsModifier / modele_champs),
// pas une édition en ligne.
//
// Une règle créée est toujours ACTIVE (CreerRegle, internal/depot/regle.go) :
// si elle recouvre une règle active existante sur la même métrique,
// l'enregistrement est refusé (ErrInvariant) en nommant la concurrente —
// affiché tel quel (messageUtilisateur, erreurs_metier.go, gère déjà
// ErrInvariant comme les autres erreurs métier « à message franc »).
func (s *serveur) routesRegles() {
	s.mux.HandleFunc("GET /regles", s.lecteur(s.reglesPage))
	s.mux.HandleFunc("GET /regles/tableau", s.lecteur(s.reglesTableau))
	s.mux.HandleFunc("POST /regles", s.editeur(s.reglesCreer))
	s.mux.HandleFunc("POST /regles/{id}/activer", s.editeur(s.reglesActiver))
	s.mux.HandleFunc("POST /regles/{id}/desactiver", s.editeur(s.reglesDesactiver))
	s.mux.HandleFunc("POST /regles/{id}/dupliquer", s.editeur(s.reglesDupliquer))

	s.mux.HandleFunc("GET /regles/{id}", s.lecteur(s.regleDetailPage))
	s.mux.HandleFunc("PUT /regles/{id}", s.editeur(s.regleModifier))
}

// ---------------------------------------------------------------- référentiels

// referentielsRegle porte les listes nécessaires aux <select> des formulaires
// de création et d'édition : la métrique, puis les six dimensions du filtre
// (mêmes selects que clusters/liste.html, plus Usage et Cluster que les
// clusters n'exposent pas comme filtre d'écran mais qui sont bien des
// dimensions du filtre d'une règle).
type referentielsRegle struct {
	Metriques      []depot.Metrique
	Projets        []depot.Projet
	Environnements []depot.Environnement
	Technos        []depot.Techno
	Tiers          []depot.Tier
	Usages         []depot.UsageFonctionnel
	Clusters       []depot.Cluster
}

func (s *serveur) chargerReferentielsRegle() (referentielsRegle, error) {
	rc, err := s.chargerReferentielsCluster()
	if err != nil {
		return referentielsRegle{}, err
	}
	metriques, err := s.depot.ListerMetriques()
	if err != nil {
		return referentielsRegle{}, err
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		return referentielsRegle{}, err
	}
	return referentielsRegle{
		Metriques: metriques, Projets: rc.Projets, Environnements: rc.Environnements,
		Technos: rc.Technos, Tiers: rc.Tiers, Usages: rc.Usages, Clusters: clusters,
	}, nil
}

// libellesRegle associe l'id de chaque dimension du filtre (et de la
// métrique) à son libellé, calculé une fois par requête — même principe que
// libellesCluster (handlers_cluster.go), dont ce type réutilise le résultat
// pour quatre des six dimensions, complété par Usage, Cluster et Métrique.
type libellesRegle struct {
	Metriques      map[int64]string
	Projets        map[int64]string
	Environnements map[int64]string
	Technos        map[int64]string
	Tiers          map[int64]string
	Usages         map[int64]string
	Clusters       map[int64]string
}

func (s *serveur) chargerLibellesRegle() (libellesRegle, error) {
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		return libellesRegle{}, err
	}
	metriques, err := s.depot.ListerMetriques()
	if err != nil {
		return libellesRegle{}, err
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{InclureInactifs: true})
	if err != nil {
		return libellesRegle{}, err
	}
	lr := libellesRegle{
		Metriques: make(map[int64]string, len(metriques)),
		Projets:   lc.Projets, Environnements: lc.Environnements,
		Technos: lc.Technos, Tiers: lc.Tiers, Usages: lc.Usages,
		Clusters: make(map[int64]string, len(clusters)),
	}
	for _, m := range metriques {
		lr.Metriques[m.ID] = m.Libelle
	}
	for _, c := range clusters {
		lr.Clusters[c.ID] = c.Nom
	}
	return lr, nil
}

// ---------------------------------------------------------------- liste

// regleLigne incorpore depot.Regle, un message d'erreur optionnel (patron
// projetLigne), le libellé de la métrique et un résumé du filtre — le
// gabarit ne doit jamais résoudre lui-même un id en libellé, même principe
// que clusterLigne (handlers_cluster.go).
type regleLigne struct {
	depot.Regle
	Erreur          string
	MetriqueLibelle string
	FiltreResume    string
}

// NiveauLibelle traduit la constante de niveau d'évaluation
// (internal/capacity, capacity.go) en texte affichable.
func (l regleLigne) NiveauLibelle() string {
	if l.NiveauEvaluation == capacity.NiveauParServeur {
		return "Par serveur"
	}
	return "Périmètre"
}

// filtreResume énumère les dimensions non vides du filtre, dans l'ordre
// projet / environnement / techno / tier / usage / cluster, jointes par
// " / " (ex. « Elastic / Hot ») — "Tous" si aucune dimension n'est
// renseignée.
func filtreResume(r depot.Regle, lr libellesRegle) string {
	var parties []string
	if r.ProjetID != nil {
		parties = append(parties, lr.Projets[*r.ProjetID])
	}
	if r.EnvironnementID != nil {
		parties = append(parties, lr.Environnements[*r.EnvironnementID])
	}
	if r.TechnoID != nil {
		parties = append(parties, lr.Technos[*r.TechnoID])
	}
	if r.TierID != nil {
		parties = append(parties, lr.Tiers[*r.TierID])
	}
	if r.UsageID != nil {
		parties = append(parties, lr.Usages[*r.UsageID])
	}
	if r.ClusterID != nil {
		parties = append(parties, lr.Clusters[*r.ClusterID])
	}
	if len(parties) == 0 {
		return "Tous"
	}
	return strings.Join(parties, " / ")
}

func regleEnLigne(r depot.Regle, lr libellesRegle) regleLigne {
	return regleLigne{
		Regle: r, MetriqueLibelle: lr.Metriques[r.MetriqueID], FiltreResume: filtreResume(r, lr),
	}
}

// reglesEnLignesTriees convertit et trie par métrique puis nom (ListerRegles
// renvoie par identifiant ; le tri d'affichage se fait ici plutôt que dans le
// dépôt, hors périmètre de cet écran).
func reglesEnLignesTriees(regles []depot.Regle, lr libellesRegle) []regleLigne {
	out := make([]regleLigne, len(regles))
	for i, r := range regles {
		out[i] = regleEnLigne(r, lr)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].MetriqueLibelle != out[j].MetriqueLibelle {
			return out[i].MetriqueLibelle < out[j].MetriqueLibelle
		}
		return out[i].Nom < out[j].Nom
	})
	return out
}

func (s *serveur) chargerReglesEnLignes() ([]regleLigne, error) {
	regles, err := s.depot.ListerRegles(0)
	if err != nil {
		return nil, err
	}
	lr, err := s.chargerLibellesRegle()
	if err != nil {
		return nil, err
	}
	return reglesEnLignesTriees(regles, lr), nil
}

func (s *serveur) reglesPage(w http.ResponseWriter, r *http.Request) {
	lignes, err := s.chargerReglesEnLignes()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : les colonnes du tableau, libellés et
	// résumé de filtre résolus comme à l'écran.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Règles", Colonnes: []string{"Nom", "Métrique", "Expression", "Niveau", "Filtre", "Actif"}}
		for _, l := range lignes {
			t.Lignes = append(t.Lignes, []any{l.Nom, l.MetriqueLibelle, l.Expression, l.NiveauLibelle(), l.FiltreResume, l.Actif})
		}
		return t, nil
	}) {
		return
	}
	ref, err := s.chargerReferentielsRegle()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.regles"), "regles_page", map[string]any{
		"Regles": lignes, "Referentiels": ref, "NiveauPerimetre": capacity.NiveauPerimetre,
		"NiveauParServeur": capacity.NiveauParServeur, "Export": exportRegles(r),
	})
}

// exportRegles : liens d'export posés dans le fragment, vers la page (voir
// exportProjets, handlers_projet.go). Pas de filtre sur cet écran.
func exportRegles(r *http.Request) exportLiens {
	return liensExportVers(r, "/regles", "")
}

func (s *serveur) reglesTableau(w http.ResponseWriter, r *http.Request) {
	s.rendreReglesTableau(w, r, "")
}

// rendreReglesTableau recharge et réaffiche la liste entière des règles —
// cible de toute mutation qui change le nombre de lignes ou leur contenu
// (création, duplication), même principe que rendreClustersTableau
// (handlers_cluster.go).
func (s *serveur) rendreReglesTableau(w http.ResponseWriter, r *http.Request, erreur string) {
	lignes, err := s.chargerReglesEnLignes()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "regles_tableau", map[string]any{
		"Regles": lignes, "Erreur": erreur, "Export": exportRegles(r),
	})
}

// regleFiltreDepuisFormulaire lit les six dimensions facultatives du filtre,
// communes à la création et à l'édition.
func regleFiltreDepuisFormulaire(r *http.Request) (depot.Regle, error) {
	var reg depot.Regle
	var err error
	if reg.ProjetID, err = idOptionnel(r, "projet_id"); err != nil {
		return reg, err
	}
	if reg.EnvironnementID, err = idOptionnel(r, "environnement_id"); err != nil {
		return reg, err
	}
	if reg.TechnoID, err = idOptionnel(r, "techno_id"); err != nil {
		return reg, err
	}
	if reg.TierID, err = idOptionnel(r, "tier_id"); err != nil {
		return reg, err
	}
	if reg.UsageID, err = idOptionnel(r, "usage_id"); err != nil {
		return reg, err
	}
	if reg.ClusterID, err = idOptionnel(r, "cluster_id"); err != nil {
		return reg, err
	}
	return reg, nil
}

// reglesCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) reglesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	reg, err := regleFiltreDepuisFormulaire(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reg.Nom = r.FormValue("nom")
	reg.MetriqueID = idRequis(r, "metrique_id")
	reg.Expression = r.FormValue("expression")
	reg.NiveauEvaluation = r.FormValue("niveau_evaluation")
	reg.ComposantOffre = texteOptionnel(r, "composant_offre")
	reg.Commentaire = texteOptionnel(r, "commentaire")

	_, errCreation := s.depotPour(r).CreerRegle(reg)
	if errCreation != nil && !erreurMetier(errCreation) {
		s.erreurServeur(w, r, errCreation)
		return
	}
	s.rendreReglesTableau(w, r, messageUtilisateur(errCreation))
}

func (s *serveur) reglesActiver(w http.ResponseWriter, r *http.Request) {
	s.reglesBasculerActivite(w, r, s.depotPour(r).ActiverRegle)
}

func (s *serveur) reglesDesactiver(w http.ResponseWriter, r *http.Request) {
	s.reglesBasculerActivite(w, r, s.depotPour(r).DesactiverRegle)
}

// reglesBasculerActivite répond la ligne seule (pas le tableau entier) :
// activer/désactiver ne change ni le nombre de lignes ni leur ordre. Un échec
// d'activation par recouvrement (ErrInvariant) affiche le message inline sur
// la ligne, sans rien avoir changé en base.
func (s *serveur) reglesBasculerActivite(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	errAction := action(id)
	if errAction != nil && !erreurMetier(errAction) {
		s.erreurServeur(w, r, errAction)
		return
	}
	reg, err := s.depot.LireRegle(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	lr, err := s.chargerLibellesRegle()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	ligne := regleEnLigne(reg, lr)
	ligne.Erreur = messageUtilisateur(errAction)
	s.rendreFragment(w, r, "regles_ligne", ligne)
}

// reglesDupliquer crée une copie inactive (DupliquerRegle, internal/depot/regle.go)
// et réaffiche la liste entière pour qu'elle y apparaisse — voir projetsCreer.
func (s *serveur) reglesDupliquer(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, errDup := s.depotPour(r).DupliquerRegle(id)
	if errDup != nil && !erreurMetier(errDup) {
		s.erreurServeur(w, r, errDup)
		return
	}
	s.rendreReglesTableau(w, r, messageUtilisateur(errDup))
}

// ---------------------------------------------------------------- détail / édition

// regleFormulaire est le contexte du gabarit "regle_champs" : la règle, son
// message d'erreur optionnel et les référentiels nécessaires à ses <select>.
type regleFormulaire struct {
	depot.Regle
	Erreur       string
	Referentiels referentielsRegle
}

// ProjetIDOuZero, EnvironnementIDOuZero, TechnoIDOuZero, TierIDOuZero,
// UsageIDOuZero et ClusterIDOuZero exposent chaque dimension du filtre
// (*int64) comme un int64 (0 = absent) pour que le gabarit compare
// directement à l'id d'une <option> avec eq — même principe que
// TierIDOuZero/UsageIDOuZero (clusterLigne, handlers_cluster.go).
func (f regleFormulaire) ProjetIDOuZero() int64        { return entierOuZero(f.ProjetID) }
func (f regleFormulaire) EnvironnementIDOuZero() int64 { return entierOuZero(f.EnvironnementID) }
func (f regleFormulaire) TechnoIDOuZero() int64        { return entierOuZero(f.TechnoID) }
func (f regleFormulaire) TierIDOuZero() int64          { return entierOuZero(f.TierID) }
func (f regleFormulaire) UsageIDOuZero() int64         { return entierOuZero(f.UsageID) }
func (f regleFormulaire) ClusterIDOuZero() int64       { return entierOuZero(f.ClusterID) }

func entierOuZero(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// regleDetailPage est une navigation de page complète (comme
// modeleDetailPage, handlers_modele.go) : un identifiant inexistant y répond
// 404 classique.
func (s *serveur) regleDetailPage(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reg, err := s.depot.LireRegle(id)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	ref, err := s.chargerReferentielsRegle()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, reg.Nom, "regle_page", map[string]any{
		"Regle":            regleFormulaire{Regle: reg, Referentiels: ref},
		"NiveauPerimetre":  capacity.NiveauPerimetre,
		"NiveauParServeur": capacity.NiveauParServeur,
	})
}

// regleModifier applique le formulaire de la page de détail. Comme
// modeleChampsModifier (handlers_modele.go), pas de bascule lecture/édition :
// le formulaire est toujours affiché, PUT /regles/{id} réaffiche le même
// fragment avec succès ou erreur.
//
// Actif n'a pas de champ visible dans ce formulaire (bascule réservée aux
// boutons Activer/Désactiver de la liste) : il voyage en champ caché pour que
// ModifierRegle, qui écrase la colonne actif contrairement à
// ModifierProjet/ModifierCluster/ModifierModele, ne désactive pas la règle
// faute d'un champ pour le reporter.
func (s *serveur) regleModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	reg, err := regleFiltreDepuisFormulaire(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reg.ID = id
	reg.Nom = r.FormValue("nom")
	reg.MetriqueID = idRequis(r, "metrique_id")
	reg.Expression = r.FormValue("expression")
	reg.NiveauEvaluation = r.FormValue("niveau_evaluation")
	reg.ComposantOffre = texteOptionnel(r, "composant_offre")
	reg.Commentaire = texteOptionnel(r, "commentaire")
	reg.Actif = r.FormValue("actif") != ""

	if errEcriture := s.depotPour(r).ModifierRegle(reg); errEcriture != nil {
		if !erreurMetier(errEcriture) {
			s.erreurServeur(w, r, errEcriture)
			return
		}
		ref, errRef := s.chargerReferentielsRegle()
		if errRef != nil {
			s.erreurServeur(w, r, errRef)
			return
		}
		s.rendreFragment(w, r, "regle_champs", regleFormulaire{
			Regle: reg, Erreur: messageUtilisateur(errEcriture), Referentiels: ref,
		})
		return
	}

	relu, err := s.depot.LireRegle(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	ref, err := s.chargerReferentielsRegle()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "regle_champs", regleFormulaire{Regle: relu, Referentiels: ref})
}
