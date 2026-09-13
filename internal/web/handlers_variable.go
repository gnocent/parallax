package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"parallax/internal/depot"
)

// routesVariables enregistre l'écran de liste des déclarations (patron de
// référence, voir handlers_projet.go, simplifié comme handlers_environnement.go :
// pas de drapeau d'activité) puis la page de détail d'une variable, qui porte
// la déclaration (formulaire toujours visible, comme modele_champs — voir
// handlers_modele.go et son commentaire de tête) et la gestion des valeurs par
// portée, le vrai cœur de l'écran.
//
// Comme pour les modèles, la liste ne fait pas de bascule lecture/édition :
// "Détail" est un lien classique vers /variables/{id}, seul endroit où la
// déclaration se modifie. Ça évite toute collision avec GET /variables/{id}
// qui est ici une page complète, pas un fragment de ligne.
//
// Convention de nommage des paramètres de chemin : {id} désigne toujours la
// variable, {valeurID} la valeur — voir idCheminNomme (handlers_modele.go).
//
// Depuis la v2.1, les valeurs sont paramétrables par scénario (paramètre
// "scenario", voir scenario_contexte.go) : ListerValeurs et DefinirValeur
// l'acceptaient déjà (le dépôt a toujours porté cette surcharge, seul l'écran
// s'y ajoute maintenant) — sous un scénario, la liste montre le réel *plus*
// les surcharges de ce scénario (lecture brute, pas de résolution : on veut
// voir les deux pour comparer), et toute nouvelle valeur ou modification
// s'écrit dans le scénario choisi plutôt que dans le réel.
func (s *serveur) routesVariables() {
	s.mux.HandleFunc("GET /variables", s.lecteur(s.variablesPage))
	s.mux.HandleFunc("GET /variables/tableau", s.lecteur(s.variablesTableau))
	s.mux.HandleFunc("POST /variables", s.editeur(s.variablesCreer))

	s.mux.HandleFunc("GET /variables/{id}", s.lecteur(s.variableDetailPage))
	s.mux.HandleFunc("PUT /variables/{id}", s.editeur(s.variableDeclarationModifier))

	s.mux.HandleFunc("POST /variables/{id}/valeurs", s.editeur(s.valeursCreerOuModifier))
	s.mux.HandleFunc("GET /variables/{id}/valeurs/{valeurID}", s.lecteur(s.valeurLigneLecture))
	s.mux.HandleFunc("GET /variables/{id}/valeurs/{valeurID}/editer", s.editeur(s.valeurLigneEdition))
	s.mux.HandleFunc("POST /variables/{id}/valeurs/{valeurID}/supprimer", s.editeur(s.valeurSupprimer))
	s.mux.HandleFunc("GET /variables/{id}/valeurs/{valeurID}/historique", s.lecteur(s.valeurHistoriquePage))
}

// ---------------------------------------------------------------- liste

// variableLigne incorpore depot.Variable et ajoute un message d'erreur
// optionnel — voir le commentaire de projetLigne dans handlers_projet.go,
// même raison.
type variableLigne struct {
	depot.Variable
	Erreur string
}

// DefautTexte expose Defaut (*float64) comme une chaîne pour le gabarit de
// liste — même raison que TierIDOuZero (clusterLigne, handlers_cluster.go) :
// html/template afficherait l'adresse mémoire d'un pointeur imprimé
// directement.
func (l variableLigne) DefautTexte() string {
	if l.Defaut == nil {
		return ""
	}
	return strconv.FormatFloat(*l.Defaut, 'f', -1, 64)
}

func variablesEnLignes(variables []depot.Variable) []variableLigne {
	out := make([]variableLigne, len(variables))
	for i, v := range variables {
		out[i] = variableLigne{Variable: v}
	}
	return out
}

func (s *serveur) variablesPage(w http.ResponseWriter, r *http.Request) {
	variables, err := s.depot.ListerVariables()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Variables", Colonnes: []string{"Code", "Libellé", "Unité", "Défaut", "Commentaire"}}
		for _, v := range variables {
			t.Lignes = append(t.Lignes, []any{v.Code, v.Libelle, texteOuVide(v.Unite), nombreOuVide(v.Defaut), texteOuVide(v.Commentaire)})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.variables"), "variables_page", map[string]any{
		"Variables": variablesEnLignes(variables), "Export": exportVariables(r),
	})
}

// exportVariables : liens d'export posés dans le fragment, vers la page (voir
// exportProjets, handlers_projet.go). Pas de filtre sur cet écran.
func exportVariables(r *http.Request) exportLiens {
	return liensExportVers(r, "/variables", "")
}

func (s *serveur) variablesTableau(w http.ResponseWriter, r *http.Request) {
	variables, err := s.depot.ListerVariables()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "variables_tableau", map[string]any{
		"Variables": variablesEnLignes(variables), "Export": exportVariables(r),
	})
}

// variablesCreer renvoie toujours le tableau entier — voir projetsCreer.
// L'unité, la valeur par défaut et le commentaire se saisissent aussi ici
// (contrairement aux modèles, la déclaration d'une variable est simple : pas
// de besoin de réserver ces champs à la page de détail).
func (s *serveur) variablesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	defaut, errDefaut := flottantOptionnelModele(r, "defaut")
	var err error
	if errDefaut != nil {
		err = errDefaut
	} else {
		_, err = s.depotPour(r).CreerVariable(depot.Variable{
			Code: r.FormValue("code"), Libelle: r.FormValue("libelle"),
			Unite: texteOptionnel(r, "unite"), Defaut: defaut,
			Commentaire: texteOptionnel(r, "commentaire"),
		})
	}
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	variables, errListe := s.depot.ListerVariables()
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "variables_tableau", map[string]any{
		"Variables": variablesEnLignes(variables), "Erreur": messageUtilisateur(err), "Export": exportVariables(r),
	})
}

// ---------------------------------------------------------------- détail : déclaration

// variableFormulaire est le contexte du gabarit "variable_champs" : même
// raison que modeleFormulaire (handlers_modele.go) — Defaut est un *float64,
// que html/template afficherait comme une adresse mémoire s'il était imprimé
// directement.
type variableFormulaire struct {
	depot.Variable
	Erreur      string
	DefautTexte string
}

func construireVariableFormulaire(v depot.Variable) variableFormulaire {
	vf := variableFormulaire{Variable: v}
	if v.Defaut != nil {
		vf.DefautTexte = strconv.FormatFloat(*v.Defaut, 'f', -1, 64)
	}
	return vf
}

// variableDetailPage est une navigation de page complète (comme
// modeleDetailPage, handlers_modele.go) : un identifiant inexistant y répond
// 404 classique.
func (s *serveur) variableDetailPage(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireVariable(id)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}

	valeursBloc, err := s.construireValeursBloc(id, scenarioDepuisRequete(r), "")
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : la table des valeurs telle qu'affichée,
	// réel plus surcharges du scénario choisi, avec le seau de chaque ligne.
	if s.exporter(w, r, func() (tableau, error) {
		return tableauValeurs(v, valeursBloc.Valeurs), nil
	}) {
		return
	}
	ref, err := s.chargerReferentielsPortee()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	s.rendrePage(w, r, v.Code, "variable_page", map[string]any{
		"Variable":     construireVariableFormulaire(v),
		"Valeurs":      valeursBloc,
		"Referentiels": ref,
	})
}

// variableDeclarationModifier traite le formulaire de déclaration de la page
// de détail. Comme modeleChampsModifier (handlers_modele.go), pas de bascule
// lecture/édition : le formulaire est toujours affiché, PUT /variables/{id}
// réaffiche le même fragment avec succès ou erreur.
func (s *serveur) variableDeclarationModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	defaut, errDefaut := flottantOptionnelModele(r, "defaut")
	v := depot.Variable{
		ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle"),
		Unite: texteOptionnel(r, "unite"), Defaut: defaut,
		Commentaire: texteOptionnel(r, "commentaire"),
	}

	var errEcriture error
	if errDefaut != nil {
		errEcriture = errDefaut
	} else {
		errEcriture = s.depotPour(r).ModifierVariable(v)
	}

	if errEcriture != nil {
		if !erreurMetier(errEcriture) {
			s.erreurServeur(w, r, errEcriture)
			return
		}
		// ModifierVariable ne touche pas le code (stable) : le relire pour ne
		// pas afficher une valeur vide sur le réaffichage après erreur — même
		// précaution que projetsModifier pour Actif.
		actuel, errLecture := s.depot.LireVariable(id)
		if errLecture == nil {
			v.Code = actuel.Code
		}
		vf := construireVariableFormulaire(v)
		vf.Erreur = messageUtilisateur(errEcriture)
		s.rendreFragment(w, r, "variable_champs", vf)
		return
	}

	relu, err := s.depot.LireVariable(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "variable_champs", construireVariableFormulaire(relu))
}

// ---------------------------------------------------------------- détail : valeurs par portée

// referentielsPortee porte les listes nécessaires aux cinq <select> de portée
// (Projet, Environnement, Techno, Tier, Cluster) du formulaire "nouvelle
// valeur" — dimensions actives seulement, comme chargerReferentielsCluster
// (handlers_cluster.go), dont ce type réutilise le résultat pour les quatre
// premières dimensions.
type referentielsPortee struct {
	Projets        []depot.Projet
	Environnements []depot.Environnement
	Technos        []depot.Techno
	Tiers          []depot.Tier
	Clusters       []depot.Cluster
}

func (s *serveur) chargerReferentielsPortee() (referentielsPortee, error) {
	rc, err := s.chargerReferentielsCluster()
	if err != nil {
		return referentielsPortee{}, err
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{})
	if err != nil {
		return referentielsPortee{}, err
	}
	return referentielsPortee{
		Projets: rc.Projets, Environnements: rc.Environnements,
		Technos: rc.Technos, Tiers: rc.Tiers, Clusters: clusters,
	}, nil
}

// libellesPortee associe l'id de chaque dimension de portée à son libellé —
// même principe que libellesCluster (handlers_cluster.go), dont ce type
// réutilise le résultat pour les quatre premières dimensions, complété par le
// nom des clusters (absent de libellesCluster).
type libellesPortee struct {
	Projets        map[int64]string
	Environnements map[int64]string
	Technos        map[int64]string
	Tiers          map[int64]string
	Clusters       map[int64]string
}

func (s *serveur) chargerLibellesPortee() (libellesPortee, error) {
	lc, err := s.chargerLibellesCluster()
	if err != nil {
		return libellesPortee{}, err
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{InclureInactifs: true})
	if err != nil {
		return libellesPortee{}, err
	}
	noms := make(map[int64]string, len(clusters))
	for _, c := range clusters {
		noms[c.ID] = c.Nom
	}
	return libellesPortee{
		Projets: lc.Projets, Environnements: lc.Environnements,
		Technos: lc.Technos, Tiers: lc.Tiers, Clusters: noms,
	}, nil
}

// valeurLigne incorpore depot.ValeurVariable, un message d'erreur optionnel
// et les libellés de chaque dimension de portée renseignée — même principe
// que clusterLigne (handlers_cluster.go) : le gabarit ne doit jamais résoudre
// lui-même un id en libellé.
type valeurLigne struct {
	depot.ValeurVariable
	Erreur               string
	ProjetLibelle        string
	EnvironnementLibelle string
	TechnoLibelle        string
	TierLibelle          string
	ClusterLibelle       string
}

// EstDuScenario indique si cette ligne est une surcharge de scénario plutôt
// qu'une valeur du réel — sert au badge affiché dans la liste quand la vue
// courante mélange les deux (voir valeursBloc.ScenarioID).
func (l valeurLigne) EstDuScenario() bool { return l.ScenarioID != nil }

// ScenarioIDTexte expose le scénario propriétaire de *cette valeur* (pas
// celui actuellement affiché dans le sélecteur de la page) pour le champ
// caché du formulaire d'édition : modifier une valeur réécrit toujours dans
// son propre seau, quel que soit le scénario choisi ailleurs sur l'écran.
// Chaîne vide (comme les autres champs cachés de portée) plutôt que "0" :
// scenarioDepuisRequete traite les deux comme le réel, mais rester cohérent
// avec ProjetIDTexte et consorts évite toute surprise si ce champ est un
// jour inspecté isolément.
func (l valeurLigne) ScenarioIDTexte() string { return texteEntierOptionnel(l.ScenarioID) }

// ProjetIDTexte, EnvironnementIDTexte, TechnoIDTexte, TierIDTexte et
// ClusterIDTexte exposent chaque dimension de portée (*int64) comme une
// chaîne pour les champs cachés du formulaire d'édition — html/template
// afficherait l'adresse mémoire d'un pointeur imprimé directement, même
// raison que DefautTexte ci-dessus et que TierIDOuZero (clusterLigne,
// handlers_cluster.go).
func (l valeurLigne) ProjetIDTexte() string { return texteEntierOptionnel(l.Portee.ProjetID) }
func (l valeurLigne) EnvironnementIDTexte() string {
	return texteEntierOptionnel(l.Portee.EnvironnementID)
}
func (l valeurLigne) TechnoIDTexte() string  { return texteEntierOptionnel(l.Portee.TechnoID) }
func (l valeurLigne) TierIDTexte() string    { return texteEntierOptionnel(l.Portee.TierID) }
func (l valeurLigne) ClusterIDTexte() string { return texteEntierOptionnel(l.Portee.ClusterID) }

func texteEntierOptionnel(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

func valeurEnLigne(v depot.ValeurVariable, lp libellesPortee) valeurLigne {
	l := valeurLigne{ValeurVariable: v}
	if v.Portee.ProjetID != nil {
		l.ProjetLibelle = lp.Projets[*v.Portee.ProjetID]
	}
	if v.Portee.EnvironnementID != nil {
		l.EnvironnementLibelle = lp.Environnements[*v.Portee.EnvironnementID]
	}
	if v.Portee.TechnoID != nil {
		l.TechnoLibelle = lp.Technos[*v.Portee.TechnoID]
	}
	if v.Portee.TierID != nil {
		l.TierLibelle = lp.Tiers[*v.Portee.TierID]
	}
	if v.Portee.ClusterID != nil {
		l.ClusterLibelle = lp.Clusters[*v.Portee.ClusterID]
	}
	return l
}

// specificitePortee évalue le nombre de dimensions renseignées, poids selon
// la hiérarchie du §7.2 (cluster > tier > techno > environnement > projet),
// pour trier les valeurs par spécificité décroissante — la portée la plus
// précise, la plus intéressante à relire, apparaît en premier de son année.
func specificitePortee(p depot.PorteeVariable) int {
	switch {
	case p.ClusterID != nil:
		return 5
	case p.TierID != nil:
		return 4
	case p.TechnoID != nil:
		return 3
	case p.EnvironnementID != nil:
		return 2
	case p.ProjetID != nil:
		return 1
	default:
		return 0
	}
}

func trierValeurs(valeurs []depot.ValeurVariable) {
	sort.SliceStable(valeurs, func(i, j int) bool {
		if valeurs[i].Annee != valeurs[j].Annee {
			return valeurs[i].Annee < valeurs[j].Annee
		}
		return specificitePortee(valeurs[i].Portee) > specificitePortee(valeurs[j].Portee)
	})
}

// valeursBloc est le contexte du gabarit "variable_valeurs" : la liste des
// valeurs du réel, plus celles du scénario choisi (lecture brute, sans
// arbitrage : on veut voir les deux pour comparer, à la différence de
// AffectationsResolues qui résout), triées par année puis spécificité
// décroissante (trierValeurs).
type valeursBloc struct {
	VariableID int64
	Valeurs    []valeurLigne
	Erreur     string
	Scenarios  []depot.Scenario
	ScenarioID int64 // 0 = réel seul, même convention que TierIDOuZero
	Export     exportLiens
}

// exportValeurs construit les liens d'export de la table des valeurs d'une
// variable : vers la page de détail, sous le scénario affiché. Calculé sans
// requête sous la main (rendreValeursTableau répond aussi à des POST), à
// partir des seuls identifiants — voir liensExportDepuis (tableau.go).
func exportValeurs(variableID int64, scenarioID *int64) exportLiens {
	q := url.Values{}
	if scenarioID != nil {
		q.Set("scenario", strconv.FormatInt(*scenarioID, 10))
	}
	return liensExportDepuis(fmt.Sprintf("/variables/%d", variableID), q, "")
}

// tableauValeurs est la forme exportable de la table des valeurs : les
// libellés de portée (jamais les identifiants), la valeur brute, et le seau
// (réel ou scénario) que le badge affiche.
func tableauValeurs(v depot.Variable, lignes []valeurLigne) tableau {
	t := tableau{Titre: "Valeurs — " + v.Code, Colonnes: []string{
		"Année", "Projet", "Environnement", "Techno", "Tier", "Cluster", "Valeur", "Commentaire", "Seau",
	}}
	for _, l := range lignes {
		seau := "réel"
		if l.EstDuScenario() {
			seau = "scénario"
		}
		t.Lignes = append(t.Lignes, []any{
			l.Annee, l.ProjetLibelle, l.EnvironnementLibelle, l.TechnoLibelle, l.TierLibelle,
			l.ClusterLibelle, l.Valeur, texteOuVide(l.Commentaire), seau,
		})
	}
	return t
}

func (s *serveur) construireValeursBloc(variableID int64, scenarioID *int64, erreur string) (valeursBloc, error) {
	valeurs, err := s.depot.ListerValeurs(variableID, scenarioID, nil)
	if err != nil {
		return valeursBloc{}, err
	}
	trierValeurs(valeurs)
	lp, err := s.chargerLibellesPortee()
	if err != nil {
		return valeursBloc{}, err
	}
	scenarios, err := s.depot.ListerScenarios(false)
	if err != nil {
		return valeursBloc{}, err
	}
	lignes := make([]valeurLigne, len(valeurs))
	for i, v := range valeurs {
		lignes[i] = valeurEnLigne(v, lp)
	}
	var scenarioIDOuZero int64
	if scenarioID != nil {
		scenarioIDOuZero = *scenarioID
	}
	return valeursBloc{
		VariableID: variableID, Valeurs: lignes, Erreur: erreur,
		Scenarios: scenarios, ScenarioID: scenarioIDOuZero,
		Export: exportValeurs(variableID, scenarioID),
	}, nil
}

// rendreValeursTableau recharge et réaffiche la liste entière des valeurs
// d'une variable — cible de toute mutation qui change le nombre de lignes ou
// leur contenu (création, modification, suppression), même principe que
// rendreComposantsTableau (handlers_modele.go) : DefinirValeur est un upsert,
// la modification d'une valeur existante n'est donc pas distinguée de la
// création d'une nouvelle à ce niveau.
func (s *serveur) rendreValeursTableau(w http.ResponseWriter, r *http.Request, variableID int64, scenarioID *int64, erreur string) {
	bloc, err := s.construireValeursBloc(variableID, scenarioID, erreur)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "variable_valeurs", bloc)
}

// porteeDepuisRequete lit les cinq champs de portée facultatifs du formulaire
// (valeur "" = pas de restriction à ce niveau). Erreur de parsing renvoyée
// telle quelle (idOptionnel, aide_formulaire.go) : elle ne peut survenir qu'à
// partir d'un <select> trafiqué, traitée en 400 brut comme clusterFiltreDepuisRequete.
func porteeDepuisRequete(r *http.Request) (depot.PorteeVariable, error) {
	var p depot.PorteeVariable
	var err error
	if p.ProjetID, err = idOptionnel(r, "projet_id"); err != nil {
		return p, err
	}
	if p.EnvironnementID, err = idOptionnel(r, "environnement_id"); err != nil {
		return p, err
	}
	if p.TechnoID, err = idOptionnel(r, "techno_id"); err != nil {
		return p, err
	}
	if p.TierID, err = idOptionnel(r, "tier_id"); err != nil {
		return p, err
	}
	if p.ClusterID, err = idOptionnel(r, "cluster_id"); err != nil {
		return p, err
	}
	return p, nil
}

// valeursCreerOuModifier traite le formulaire "nouvelle valeur" et le
// formulaire d'édition d'une valeur existante : les deux soumettent à cette
// même route (POST /variables/{id}/valeurs), DefinirValeur étant un upsert
// par portée exacte, *et* par scénario (voir son doc-comment,
// internal/depot/variable.go) — le formulaire d'édition (valeur_ligne_edition)
// porte les champs de portée, l'année et le scénario en champs cachés,
// identiques à la valeur d'origine, pour que l'upsert cible la même ligne
// plutôt que d'en créer une nouvelle.
//
// Le champ "scenario" (0 = réel) détermine où s'écrit la valeur, exactement
// comme pour l'affectation d'un serveur (voir handlers_serveur.go) :
// utilisateurID reste nil (pas de fil d'audit utilisateur sur le web en v1,
// c'est admis — voir la tâche).
func (s *serveur) valeursCreerOuModifier(w http.ResponseWriter, r *http.Request) {
	variableID, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	portee, errPortee := porteeDepuisRequete(r)
	if errPortee != nil {
		http.Error(w, errPortee.Error(), http.StatusBadRequest)
		return
	}
	scenarioID := scenarioDepuisRequete(r)

	annee, errAnnee := strconv.Atoi(r.FormValue("annee"))
	valeur, errValeur := strconv.ParseFloat(r.FormValue("valeur"), 64)

	var errEcriture error
	switch {
	case errAnnee != nil:
		errEcriture = fmt.Errorf("%w : « %s » n'est pas une année valide", depot.ErrValidation, r.FormValue("annee"))
	case errValeur != nil:
		errEcriture = fmt.Errorf("%w : « %s » n'est pas une valeur numérique valide", depot.ErrValidation, r.FormValue("valeur"))
	default:
		_, errEcriture = s.depotPour(r).DefinirValeur(variableID, scenarioID, annee, portee, valeur,
			texteOptionnel(r, "commentaire"), nil)
	}
	if errEcriture != nil && !erreurMetier(errEcriture) {
		s.erreurServeur(w, r, errEcriture)
		return
	}
	s.rendreValeursTableau(w, r, variableID, scenarioID, messageUtilisateur(errEcriture))
}

func (s *serveur) valeurLigneLecture(w http.ResponseWriter, r *http.Request) {
	valeurID, err := idCheminNomme(r, "valeurID")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireValeurParID(valeurID)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	lp, err := s.chargerLibellesPortee()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "valeur_ligne", valeurEnLigne(v, lp))
}

func (s *serveur) valeurLigneEdition(w http.ResponseWriter, r *http.Request) {
	valeurID, err := idCheminNomme(r, "valeurID")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireValeurParID(valeurID)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	lp, err := s.chargerLibellesPortee()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "valeur_ligne_edition", valeurEnLigne(v, lp))
}

// valeurSupprimer renvoie toujours le tableau entier — voir
// rendreValeursTableau.
func (s *serveur) valeurSupprimer(w http.ResponseWriter, r *http.Request) {
	variableID, errV := idChemin(r)
	valeurID, errID := idCheminNomme(r, "valeurID")
	if errV != nil || errID != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	err := s.depotPour(r).SupprimerValeur(valeurID)
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreValeursTableau(w, r, variableID, scenarioDepuisRequete(r), messageUtilisateur(err))
}

// valeurHistoriquePage est une petite page complète listant les écrasements
// successifs d'une valeur — navigation classique (lien, pas htmx), comme
// modeleDetailPage : un identifiant inexistant y répond 404 classique.
func (s *serveur) valeurHistoriquePage(w http.ResponseWriter, r *http.Request) {
	variableID, errVar := idChemin(r)
	valeurID, errID := idCheminNomme(r, "valeurID")
	if errVar != nil || errID != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireVariable(variableID)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	valeur, err := s.depot.LireValeurParID(valeurID)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	historique, err := s.depot.HistoriqueValeur(valeurID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : les écrasements successifs de la valeur.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Historique — " + v.Code, Colonnes: []string{"Horodatage", "Ancienne valeur", "Nouvelle valeur"}}
		for _, h := range historique {
			t.Lignes = append(t.Lignes, []any{h.Horodatage, h.Ancienne, h.Nouvelle})
		}
		return t, nil
	}) {
		return
	}
	lp, err := s.chargerLibellesPortee()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.historique")+" — "+v.Code, "valeur_historique_page", map[string]any{
		"Variable":   v,
		"Valeur":     valeurEnLigne(valeur, lp),
		"Historique": historique,
		"Export":     liensExport(r),
	})
}
