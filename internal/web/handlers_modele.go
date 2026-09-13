package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"parallax/internal/depot"
)

// routesModeles enregistre les écrans du catalogue matériel : la liste des
// modèles (patron de référence, voir handlers_projet.go, avec Archiver /
// Réactiver comme technos), puis la page de détail d'un modèle qui porte les
// champs financiers, les révisions, leurs composants et leurs nœuds par
// technologie.
//
// Convention de nommage des paramètres de chemin, pour les routes imbriquées
// sous un modèle : {id} désigne toujours le modèle (comme dans le patron de
// référence), {revisionID} la révision, {compID} le composant, {technoID} la
// techno. idChemin (erreurs_metier.go) lit {id} ; idCheminNomme ci-dessous
// lit les autres.
//
// Le point sensible est l'immuabilité des révisions (CLAUDE.md « Les
// révisions de modèle sont immuables », docs/modele-donnees.md §12 invariant
// 1) : voir le commentaire en tête de templates/modeles/detail.html.
func (s *serveur) routesModeles() {
	s.mux.HandleFunc("GET /modeles", s.lecteur(s.modelesPage))
	s.mux.HandleFunc("GET /modeles/tableau", s.lecteur(s.modelesTableau))
	s.mux.HandleFunc("POST /modeles", s.editeur(s.modelesCreer))
	s.mux.HandleFunc("POST /modeles/{id}/archiver", s.editeur(s.modelesArchiver))
	s.mux.HandleFunc("POST /modeles/{id}/reactiver", s.editeur(s.modelesReactiver))

	s.mux.HandleFunc("GET /modeles/{id}", s.lecteur(s.modeleDetailPage))
	s.mux.HandleFunc("PUT /modeles/{id}", s.editeur(s.modeleChampsModifier))

	s.mux.HandleFunc("GET /modeles/{id}/revisions", s.lecteur(s.revisionsTableau))
	s.mux.HandleFunc("POST /modeles/{id}/revisions", s.editeur(s.revisionsCreer))
	s.mux.HandleFunc("GET /modeles/{id}/revisions/{revisionID}", s.lecteur(s.revisionBlocLecture))
	s.mux.HandleFunc("GET /modeles/{id}/revisions/{revisionID}/editer", s.editeur(s.revisionBlocEdition))
	s.mux.HandleFunc("PUT /modeles/{id}/revisions/{revisionID}", s.editeur(s.revisionModifier))

	s.mux.HandleFunc("POST /modeles/{id}/revisions/{revisionID}/composants", s.editeur(s.composantsCreer))
	s.mux.HandleFunc("GET /modeles/{id}/revisions/{revisionID}/composants/{compID}", s.lecteur(s.composantLigneLecture))
	s.mux.HandleFunc("GET /modeles/{id}/revisions/{revisionID}/composants/{compID}/editer", s.editeur(s.composantFormulaireEdition))
	s.mux.HandleFunc("PUT /modeles/{id}/revisions/{revisionID}/composants/{compID}", s.editeur(s.composantModifier))
	s.mux.HandleFunc("POST /modeles/{id}/revisions/{revisionID}/composants/{compID}/supprimer", s.editeur(s.composantSupprimer))

	s.mux.HandleFunc("PUT /modeles/{id}/revisions/{revisionID}/noeuds/{technoID}", s.editeur(s.noeudDefinir))
}

// idCheminNomme lit et parse un paramètre de chemin autre que {id} — les
// routes du catalogue en portent plusieurs à la fois (modèle, révision,
// composant, techno). Même comportement que idChemin (erreurs_metier.go).
func idCheminNomme(r *http.Request, nom string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(nom), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("identifiant « %s » invalide dans l'adresse", nom)
	}
	return id, nil
}

// chaineOptionnelleModele lit un champ de formulaire et renvoie nil si absent
// ou vide — voir siteFormulaire (handlers_zone.go), même besoin pour
// tous les champs facultatifs du catalogue (description, libellé et
// commentaire de révision, commentaire de composant…).
func chaineOptionnelleModele(r *http.Request, champ string) *string {
	v := r.FormValue(champ)
	if v == "" {
		return nil
	}
	return &v
}

// entierOptionnelModele et flottantOptionnelModele lisent un champ numérique
// facultatif du formulaire des champs financiers du modèle. Un champ vide
// donne (nil, nil) ; un champ mal formé est enveloppé dans ErrValidation pour
// que erreurMetier/messageUtilisateur (erreurs_metier.go) le traitent comme
// n'importe quelle autre erreur métier — jamais un 500 pour une faute de
// saisie utilisateur.
func entierOptionnelModele(r *http.Request, champ string) (*int64, error) {
	v := r.FormValue(champ)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w : « %s » n'est pas un entier valide pour %s", depot.ErrValidation, v, champ)
	}
	return &n, nil
}

func flottantOptionnelModele(r *http.Request, champ string) (*float64, error) {
	v := r.FormValue(champ)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil, fmt.Errorf("%w : « %s » n'est pas un nombre valide pour %s", depot.ErrValidation, v, champ)
	}
	return &n, nil
}

// corrigerFormulaire lit la case à cocher « corriger une erreur de saisie »,
// commune aux formulaires de révision, de composant et de nœuds. Jamais
// cochée par défaut : une case absente du formulaire (donc non cochée) vaut
// false, ce qui déclenche le refus ErrImmuable sur une révision référencée —
// c'est le comportement voulu.
func corrigerFormulaire(r *http.Request) bool {
	return r.FormValue("corriger") != ""
}

// messageUtilisateurRevision affine messageUtilisateur (erreurs_metier.go)
// pour le seul cas ErrImmuable non corrigé sur une révision : le message
// générique du dépôt ("modification de la révision N : révision immuable")
// n'explique pas la marche à suivre. Le texte ci-dessous est celui prescrit
// pour cet écran — les composants et les nœuds, moins visibles, gardent le
// message générique.
func messageUtilisateurRevision(err error) string {
	if errors.Is(err, depot.ErrImmuable) {
		return "Cette révision est déjà utilisée par du matériel en service. " +
			"Créez une nouvelle révision pour une évolution réelle, ou cochez la correction si c'est une simple faute de frappe."
	}
	return messageUtilisateur(err)
}

// repondreIntrouvableBloc réagit à ErrIntrouvable pour les fragments
// organisés en <div> plutôt qu'en <tr> — le bloc d'une révision entière.
// Même esprit que s.repondreIntrouvable (erreurs_metier.go), adapté au
// conteneur : cette fonction ne modifie pas erreurs_metier.go, qui est hors
// périmètre de cet écran.
func (s *serveur) repondreIntrouvableBloc(w http.ResponseWriter, r *http.Request, err error) {
	if !errors.Is(err, depot.ErrIntrouvable) {
		s.erreurServeur(w, r, err)
		return
	}
	pasDeCache(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<div class="message-erreur">Révision introuvable — recharger la page.</div>`)
}

// ---------------------------------------------------------------- modèles

// modeleLigne incorpore depot.Modele et ajoute un message d'erreur optionnel
// — voir le commentaire de projetLigne dans handlers_projet.go, même raison.
type modeleLigne struct {
	depot.Modele
	Erreur string
}

func modelesEnLignes(modeles []depot.Modele) []modeleLigne {
	out := make([]modeleLigne, len(modeles))
	for i, m := range modeles {
		out[i] = modeleLigne{Modele: m}
	}
	return out
}

// finLeaseAffichage calcule la date de fin de lease à afficher, ou une chaîne
// vide si l'un des deux champs qui la déterminent n'est pas renseigné —
// aucune erreur ne doit remonter jusqu'à l'affichage pour ce seul champ
// dérivé.
func finLeaseAffichage(m depot.Modele) string {
	if m.DateDebutLease == nil || m.DureeLeaseMois == nil {
		return ""
	}
	fin, err := depot.FinLease(*m.DateDebutLease, *m.DureeLeaseMois)
	if err != nil {
		return ""
	}
	return fin
}

// modeleFormulaire est le contexte du gabarit "modele_champs" : depot.Modele
// porte les champs numériques facultatifs en pointeurs, que html/template
// afficherait comme une adresse mémoire s'ils étaient imprimés directement
// (fmt formate un pointeur non-nil vers un type de base par son adresse, pas
// sa valeur) — d'où ces champs Texte, remplis une fois pour toutes ici.
type modeleFormulaire struct {
	depot.Modele
	Erreur                 string
	FinLease               string
	DureeLeaseMoisTexte    string
	PrixFournisseurHTTexte string
	CoutAnnuelHTTexte      string
	DureeCoutAnneesTexte   string
}

func construireModeleFormulaire(m depot.Modele) modeleFormulaire {
	mf := modeleFormulaire{Modele: m, FinLease: finLeaseAffichage(m)}
	if m.DureeLeaseMois != nil {
		mf.DureeLeaseMoisTexte = strconv.FormatInt(*m.DureeLeaseMois, 10)
	}
	if m.PrixFournisseurHT != nil {
		mf.PrixFournisseurHTTexte = strconv.FormatFloat(*m.PrixFournisseurHT, 'f', -1, 64)
	}
	if m.CoutAnnuelHT != nil {
		mf.CoutAnnuelHTTexte = strconv.FormatFloat(*m.CoutAnnuelHT, 'f', -1, 64)
	}
	if m.DureeCoutAnnees != nil {
		mf.DureeCoutAnneesTexte = strconv.FormatInt(*m.DureeCoutAnnees, 10)
	}
	return mf
}

func (s *serveur) modelesPage(w http.ResponseWriter, r *http.Request) {
	modeles, err := s.depot.ListerModeles(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données, même case « inclure les
	// archivés » que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Modèles", Colonnes: []string{"Code", "Type", "Année", "Financement", "Actif"}}
		for _, m := range modeles {
			t.Lignes = append(t.Lignes, []any{m.Code, m.Type, m.Annee, texteOuVide(m.ModeFinancement), m.Actif})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.modeles"), "modeles_page", map[string]any{
		"Modeles": modelesEnLignes(modeles), "Export": exportModeles(r),
	})
}

// exportModeles : liens d'export posés dans le fragment, vers la page, avec
// l'état de la case « inclure les archivés » (voir exportProjets).
func exportModeles(r *http.Request) exportLiens {
	return liensExportVers(r, "/modeles", "", "inclure_inactifs")
}

func (s *serveur) modelesTableau(w http.ResponseWriter, r *http.Request) {
	modeles, err := s.depot.ListerModeles(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "modeles_tableau", map[string]any{
		"Modeles": modelesEnLignes(modeles), "Export": exportModeles(r),
	})
}

// modelesCreer renvoie toujours le tableau entier — voir projetsCreer. Seuls
// type, année, code et mode de financement se saisissent ici ; les champs
// financiers se saisissent sur la page de détail (modeleChampsModifier).
func (s *serveur) modelesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	annee, errAnnee := strconv.ParseInt(r.FormValue("annee"), 10, 64)
	var err error
	if errAnnee != nil {
		err = fmt.Errorf("%w : « %s » n'est pas une année valide", depot.ErrValidation, r.FormValue("annee"))
	} else {
		_, err = s.depotPour(r).CreerModele(depot.Modele{
			Type:            r.FormValue("type"),
			Annee:           annee,
			Code:            r.FormValue("code"),
			ModeFinancement: chaineOptionnelleModele(r, "mode_financement"),
		})
	}
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	modeles, errListe := s.depot.ListerModeles(inclureInactifs(r))
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "modeles_tableau", map[string]any{
		"Modeles": modelesEnLignes(modeles), "Erreur": messageUtilisateur(err), "Export": exportModeles(r),
	})
}

func (s *serveur) modelesArchiver(w http.ResponseWriter, r *http.Request) {
	s.modelesBasculerActivite(w, r, s.depotPour(r).ArchiverModele)
}

func (s *serveur) modelesReactiver(w http.ResponseWriter, r *http.Request) {
	s.modelesBasculerActivite(w, r, s.depotPour(r).ReactiverModele)
}

func (s *serveur) modelesBasculerActivite(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := action(id); err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	m, err := s.depot.LireModele(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "modeles_ligne", modeleLigne{Modele: m})
}

// ---------------------------------------------------------------- détail du modèle

// modeleDetailPage est une navigation de page complète (le lien "Détail" de
// la liste n'est pas htmx), pas un endpoint de mutation htmx : un identifiant
// inexistant y répond 404 classique, la règle « jamais 4xx sur un endpoint
// htmx » ne s'applique qu'aux fragments.
func (s *serveur) modeleDetailPage(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m, err := s.depot.LireModele(id)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			http.NotFound(w, r)
			return
		}
		s.erreurServeur(w, r, err)
		return
	}

	revisions, err := s.depot.ListerRevisions(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	blocs, err := s.chargerRevisionBlocs(revisions)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	// export universel (tableau.go), deux tableaux nommés : les composants et
	// les nœuds par techno. Chaque bloc de révision pose ses boutons avec
	// ?revision=<id> pour n'exporter que lui ; sans ce paramètre, toutes les
	// révisions du modèle sortent, la colonne Révision les distingue.
	revisionChoisie, _ := strconv.ParseInt(r.URL.Query().Get("revision"), 10, 64)
	if s.exporterNomme(w, r, "composants", func() (tableau, error) {
		return tableauComposants(m, blocs, revisionChoisie), nil
	}) {
		return
	}
	if s.exporterNomme(w, r, "noeuds", func() (tableau, error) {
		return tableauNoeuds(m, blocs, revisionChoisie), nil
	}) {
		return
	}

	s.rendrePage(w, r, m.Code, "modele_page", map[string]any{
		"Modele":        construireModeleFormulaire(m),
		"RevisionsBloc": revisionsBloc{ModeleID: id, Revisions: blocs},
	})
}

// modeleChampsModifier traite le formulaire complet de la page de détail
// (financement, coûts, description). Contrairement à projetsModifier, il n'y
// a pas de bascule lecture/édition : le formulaire est toujours affiché,
// PUT /modeles/{id} réaffiche le même fragment avec succès ou erreur.
func (s *serveur) modeleChampsModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	annee, errAnnee := strconv.ParseInt(r.FormValue("annee"), 10, 64)
	dureeLease, errDL := entierOptionnelModele(r, "duree_lease_mois")
	prixFournisseur, errPF := flottantOptionnelModele(r, "prix_fournisseur_ht")
	coutAnnuel, errCA := flottantOptionnelModele(r, "cout_annuel_ht")
	dureeCout, errZone := entierOptionnelModele(r, "duree_cout_annees")

	m := depot.Modele{
		ID:                id,
		Type:              r.FormValue("type"),
		Annee:             annee,
		Code:              r.FormValue("code"),
		Description:       chaineOptionnelleModele(r, "description"),
		ModeFinancement:   chaineOptionnelleModele(r, "mode_financement"),
		DureeLeaseMois:    dureeLease,
		DateDebutLease:    chaineOptionnelleModele(r, "date_debut_lease"),
		PrixFournisseurHT: prixFournisseur,
		CoutAnnuelHT:      coutAnnuel,
		DureeCoutAnnees:   dureeCout,
	}

	var errEcriture error
	switch {
	case errAnnee != nil:
		errEcriture = fmt.Errorf("%w : « %s » n'est pas une année valide", depot.ErrValidation, r.FormValue("annee"))
	case errDL != nil:
		errEcriture = errDL
	case errPF != nil:
		errEcriture = errPF
	case errCA != nil:
		errEcriture = errCA
	case errZone != nil:
		errEcriture = errZone
	default:
		errEcriture = s.depotPour(r).ModifierModele(m)
	}

	if errEcriture != nil {
		if !erreurMetier(errEcriture) {
			s.erreurServeur(w, r, errEcriture)
			return
		}
		// ModifierModele ne touche pas actif : le relire pour ne pas afficher
		// un statut faux (m.Actif serait la valeur zéro Go, donc "archivé",
		// sur toute erreur de validation) — même précaution que projetsModifier.
		actuel, errLecture := s.depot.LireModele(id)
		if errLecture == nil {
			m.Actif = actuel.Actif
		}
		mf := construireModeleFormulaire(m)
		mf.Erreur = messageUtilisateur(errEcriture)
		s.rendreFragment(w, r, "modele_champs", mf)
		return
	}

	relu, err := s.depot.LireModele(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "modele_champs", construireModeleFormulaire(relu))
}

// exportRevision construit les liens d'export d'un tableau (composants ou
// noeuds) d'une révision : vers la page de détail du modèle, restreints à
// cette révision. Calculé à partir des seuls identifiants — les blocs sont
// aussi rendus en réponse à des POST (ajout, suppression), sans requête GET
// sous la main (voir liensExportDepuis, tableau.go).
func exportRevision(modeleID, revisionID int64, nom string) exportLiens {
	return liensExportDepuis(fmt.Sprintf("/modeles/%d", modeleID),
		url.Values{"revision": {strconv.FormatInt(revisionID, 10)}}, nom)
}

// tableauComposants est la forme exportable des composants des révisions d'un
// modèle — celles dont l'identifiant est revisionID, ou toutes si 0.
func tableauComposants(m depot.Modele, blocs []revisionBloc, revisionID int64) tableau {
	t := tableau{Titre: "Composants — " + m.Code, Colonnes: []string{
		"Révision", "Nature", "Code", "Quantité", "Capacité unitaire", "Unité", "Commentaire",
	}}
	for _, b := range blocs {
		if revisionID != 0 && b.ID != revisionID {
			continue
		}
		for _, c := range b.ComposantsBloc.Composants {
			t.Lignes = append(t.Lignes, []any{b.Numero, c.Nature, c.Code, c.Quantite, c.CapaciteUnitaire, c.Unite, texteOuVide(c.Commentaire)})
		}
	}
	return t
}

// tableauNoeuds est la forme exportable des nœuds installables par techno des
// révisions d'un modèle — même sélection que tableauComposants.
func tableauNoeuds(m depot.Modele, blocs []revisionBloc, revisionID int64) tableau {
	t := tableau{Titre: "Nœuds — " + m.Code, Colonnes: []string{"Révision", "Techno", "Code techno", "Nombre de nœuds"}}
	for _, b := range blocs {
		if revisionID != 0 && b.ID != revisionID {
			continue
		}
		for _, n := range b.NoeudsBloc.Noeuds {
			t.Lignes = append(t.Lignes, []any{b.Numero, n.TechnoLibelle, n.TechnoCode, n.NbNoeuds})
		}
	}
	return t
}

// ---------------------------------------------------------------- révisions

// revisionsBloc est le contexte du gabarit "modele_revisions" : la liste des
// révisions d'un modèle, chacune déjà assemblée avec ses composants et ses
// nœuds (revisionBloc) pour que le gabarit n'ait besoin d'aucune fonction de
// composition de contexte ("dict") au-delà de ce que fournit html/template.
type revisionsBloc struct {
	ModeleID  int64
	Revisions []revisionBloc
	Erreur    string
}

// revisionBloc est le contexte du gabarit "revision_bloc" : une révision, son
// statut de référencement (immuabilité), un indicateur d'édition, et ses
// composants et nœuds déjà chargés. Editant bascule entre l'affichage en
// lecture et le formulaire d'édition, tout en gardant composants et nœuds
// visibles dans les deux cas.
type revisionBloc struct {
	depot.Revision
	Referencee     bool
	Editant        bool
	Erreur         string
	Avertissement  string
	ComposantsBloc composantsBloc
	NoeudsBloc     noeudsBloc
}

// chargerRevisionBloc assemble le contexte complet d'une révision : son
// statut de référencement, ses composants et une ligne par techno active pour
// les nœuds (valeur 0 si jamais définie pour cette révision).
func (s *serveur) chargerRevisionBloc(rev depot.Revision, editant bool) (revisionBloc, error) {
	referencee, err := s.depot.RevisionEstReferencee(rev.ID)
	if err != nil {
		return revisionBloc{}, err
	}
	composants, err := s.depot.ListerComposants(rev.ID)
	if err != nil {
		return revisionBloc{}, err
	}
	technos, err := s.depot.ListerTechnos(false)
	if err != nil {
		return revisionBloc{}, err
	}
	noeuds, err := s.depot.ListerNoeuds(rev.ID)
	if err != nil {
		return revisionBloc{}, err
	}

	parTechno := make(map[int64]int64, len(noeuds))
	for _, n := range noeuds {
		parTechno[n.TechnoID] = n.NbNoeuds
	}
	lignesNoeuds := make([]noeudLigne, 0, len(technos))
	for _, t := range technos {
		lignesNoeuds = append(lignesNoeuds, noeudLigne{
			ModeleID:      rev.ModeleID,
			RevisionID:    rev.ID,
			TechnoID:      t.ID,
			TechnoCode:    t.Code,
			TechnoLibelle: t.Libelle,
			NbNoeuds:      parTechno[t.ID],
		})
	}

	return revisionBloc{
		Revision:   rev,
		Referencee: referencee,
		Editant:    editant,
		ComposantsBloc: composantsBloc{
			ModeleID: rev.ModeleID, RevisionID: rev.ID, Composants: composantsEnLignes(rev.ModeleID, composants),
			Export: exportRevision(rev.ModeleID, rev.ID, "composants"),
		},
		NoeudsBloc: noeudsBloc{
			ModeleID: rev.ModeleID, RevisionID: rev.ID, Noeuds: lignesNoeuds,
			Export: exportRevision(rev.ModeleID, rev.ID, "noeuds"),
		},
	}, nil
}

func (s *serveur) chargerRevisionBlocs(revisions []depot.Revision) ([]revisionBloc, error) {
	blocs := make([]revisionBloc, 0, len(revisions))
	for _, rev := range revisions {
		bloc, err := s.chargerRevisionBloc(rev, false)
		if err != nil {
			return nil, err
		}
		blocs = append(blocs, bloc)
	}
	return blocs, nil
}

// rendreRevisionsTableau recharge et réaffiche la liste entière des révisions
// d'un modèle — cible de POST /modeles/{id}/revisions (création) et de
// GET /modeles/{id}/revisions.
func (s *serveur) rendreRevisionsTableau(w http.ResponseWriter, r *http.Request, modeleID int64, erreur string) {
	revisions, err := s.depot.ListerRevisions(modeleID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	blocs, err := s.chargerRevisionBlocs(revisions)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "modele_revisions", revisionsBloc{ModeleID: modeleID, Revisions: blocs, Erreur: erreur})
}

func (s *serveur) revisionsTableau(w http.ResponseWriter, r *http.Request) {
	modeleID, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.rendreRevisionsTableau(w, r, modeleID, "")
}

// revisionsCreer renvoie toujours la liste entière des révisions — voir
// projetsCreer. C'est l'action « créer une révision », distincte de la
// correction d'une révision existante (revisionModifier).
func (s *serveur) revisionsCreer(w http.ResponseWriter, r *http.Request) {
	modeleID, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	_, err = s.depotPour(r).CreerRevision(depot.Revision{
		ModeleID:    modeleID,
		Libelle:     chaineOptionnelleModele(r, "libelle"),
		DateEffet:   r.FormValue("date_effet"),
		Commentaire: chaineOptionnelleModele(r, "commentaire"),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreRevisionsTableau(w, r, modeleID, messageUtilisateur(err))
}

// revisionBlocLecture répond le bloc d'une révision en lecture — sert au
// "Annuler" du formulaire d'édition.
func (s *serveur) revisionBlocLecture(w http.ResponseWriter, r *http.Request) {
	revisionID, err := idCheminNomme(r, "revisionID")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rev, err := s.depot.LireRevision(revisionID)
	if err != nil {
		s.repondreIntrouvableBloc(w, r, err)
		return
	}
	bloc, err := s.chargerRevisionBloc(rev, false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "revision_bloc", bloc)
}

// revisionBlocEdition répond le bloc d'une révision avec son formulaire
// d'édition affiché — la case « corriger » n'y est jamais précochée.
func (s *serveur) revisionBlocEdition(w http.ResponseWriter, r *http.Request) {
	revisionID, err := idCheminNomme(r, "revisionID")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rev, err := s.depot.LireRevision(revisionID)
	if err != nil {
		s.repondreIntrouvableBloc(w, r, err)
		return
	}
	bloc, err := s.chargerRevisionBloc(rev, true)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "revision_bloc", bloc)
}

// revisionModifier applique le formulaire d'édition d'une révision, en
// respectant l'invariant d'immuabilité (CLAUDE.md, docs/modele-donnees.md
// §12 invariant 1) :
//
//   - case « corriger » décochée + révision référencée par du matériel :
//     ModifierRevision renvoie ErrImmuable, rien n'est appliqué, le
//     formulaire d'édition se réaffiche avec le message explicite.
//   - case décochée + révision non référencée : s'applique normalement, sans
//     avertissement.
//   - case cochée : s'applique toujours ; si ModifierRevision signale
//     l'avertissement (révision déjà référencée écrasée), un bandeau visible
//     l'affiche — jamais silencieux.
func (s *serveur) revisionModifier(w http.ResponseWriter, r *http.Request) {
	revisionID, err := idCheminNomme(r, "revisionID")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	rev := depot.Revision{
		ID:          revisionID,
		Libelle:     chaineOptionnelleModele(r, "libelle"),
		DateEffet:   r.FormValue("date_effet"),
		Commentaire: chaineOptionnelleModele(r, "commentaire"),
	}
	avertissement, err := s.depotPour(r).ModifierRevision(rev, corrigerFormulaire(r))
	if err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		// Numero et ModeleID ne sont pas dans le formulaire : les relire pour
		// ne pas afficher un bloc incohérent (numéro à zéro, lien cassé) sur
		// le réaffichage après refus — même précaution que projetsModifier.
		actuel, errLecture := s.depot.LireRevision(revisionID)
		if errLecture == nil {
			rev.ModeleID = actuel.ModeleID
			rev.Numero = actuel.Numero
		}
		bloc, errBloc := s.chargerRevisionBloc(rev, true)
		if errBloc != nil {
			s.erreurServeur(w, r, errBloc)
			return
		}
		bloc.Erreur = messageUtilisateurRevision(err)
		s.rendreFragment(w, r, "revision_bloc", bloc)
		return
	}

	relu, err := s.depot.LireRevision(revisionID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	bloc, err := s.chargerRevisionBloc(relu, false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if avertissement {
		bloc.Avertissement = "Correction appliquée à une révision déjà référencée par du matériel — vérifiez que l'historique reste cohérent."
	}
	s.rendreFragment(w, r, "revision_bloc", bloc)
}

// ---------------------------------------------------------------- composants

// composantsBloc est le contexte du gabarit "composants_tableau".
type composantsBloc struct {
	ModeleID   int64
	RevisionID int64
	Composants []composantLigne
	Erreur     string
	Export     exportLiens // vers /modeles/{id}?tableau=composants&revision=…
}

// composantLigne incorpore depot.Composant, le ModeleID (absent de
// depot.Composant mais nécessaire aux liens/actions du gabarit) et un message
// d'erreur optionnel — même raison que projetLigne (handlers_projet.go).
type composantLigne struct {
	depot.Composant
	ModeleID int64
	Erreur   string
}

func composantsEnLignes(modeleID int64, composants []depot.Composant) []composantLigne {
	out := make([]composantLigne, len(composants))
	for i, c := range composants {
		out[i] = composantLigne{Composant: c, ModeleID: modeleID}
	}
	return out
}

// trouverComposant cherche un composant par identifiant dans une liste déjà
// chargée — le dépôt composant n'expose pas de lecture unitaire (seulement
// Ajouter/Modifier/Supprimer/Lister, voir internal/depot/composant.go), et
// modifier le dépôt est hors périmètre de cet écran.
func trouverComposant(composants []depot.Composant, id int64) (depot.Composant, bool) {
	for _, c := range composants {
		if c.ID == id {
			return c, true
		}
	}
	return depot.Composant{}, false
}

// rendreComposantsTableau recharge et réaffiche la table des composants d'une
// révision — cible des mutations composant qui changent le nombre de lignes
// (ajout, suppression) ; voir projetsCreer pour le principe.
func (s *serveur) rendreComposantsTableau(w http.ResponseWriter, r *http.Request, modeleID, revisionID int64, erreur string) {
	composants, err := s.depot.ListerComposants(revisionID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "composants_tableau", composantsBloc{
		ModeleID: modeleID, RevisionID: revisionID, Composants: composantsEnLignes(modeleID, composants), Erreur: erreur,
		Export: exportRevision(modeleID, revisionID, "composants"),
	})
}

// composantsCreer est l'action « ajouter » ; la case « corriger » y est
// disponible comme sur toute mutation de la composition d'une révision.
func (s *serveur) composantsCreer(w http.ResponseWriter, r *http.Request) {
	modeleID, errM := idChemin(r)
	revisionID, errR := idCheminNomme(r, "revisionID")
	if errM != nil || errR != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	quantite, errQ := strconv.ParseFloat(r.FormValue("quantite"), 64)
	capacite, errC := strconv.ParseFloat(r.FormValue("capacite_unitaire"), 64)
	var err error
	if errQ != nil || errC != nil {
		err = fmt.Errorf("%w : quantité et capacité unitaire doivent être des nombres", depot.ErrValidation)
	} else {
		_, err = s.depotPour(r).AjouterComposant(depot.Composant{
			RevisionID:       revisionID,
			Nature:           r.FormValue("nature"),
			Code:             r.FormValue("code"),
			Quantite:         quantite,
			CapaciteUnitaire: capacite,
			Unite:            r.FormValue("unite"),
			Commentaire:      chaineOptionnelleModele(r, "commentaire"),
		}, corrigerFormulaire(r))
	}
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreComposantsTableau(w, r, modeleID, revisionID, messageUtilisateur(err))
}

func (s *serveur) composantLigneLecture(w http.ResponseWriter, r *http.Request) {
	modeleID, errM := idChemin(r)
	revisionID, errR := idCheminNomme(r, "revisionID")
	compID, errC := idCheminNomme(r, "compID")
	if errM != nil || errR != nil || errC != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	composants, err := s.depot.ListerComposants(revisionID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	c, trouve := trouverComposant(composants, compID)
	if !trouve {
		s.repondreIntrouvable(w, r, fmt.Errorf("composant %d : %w", compID, depot.ErrIntrouvable))
		return
	}
	s.rendreFragment(w, r, "composants_ligne", composantLigne{Composant: c, ModeleID: modeleID})
}

func (s *serveur) composantFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	modeleID, errM := idChemin(r)
	revisionID, errR := idCheminNomme(r, "revisionID")
	compID, errC := idCheminNomme(r, "compID")
	if errM != nil || errR != nil || errC != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	composants, err := s.depot.ListerComposants(revisionID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	c, trouve := trouverComposant(composants, compID)
	if !trouve {
		s.repondreIntrouvable(w, r, fmt.Errorf("composant %d : %w", compID, depot.ErrIntrouvable))
		return
	}
	s.rendreFragment(w, r, "composants_ligne_edition", composantLigne{Composant: c, ModeleID: modeleID})
}

// composantModifier est l'action « modifier » — la case « corriger » est
// portée par la ligne d'édition (voir templates/modeles/detail.html) et
// ramassée par hx-include="closest tr" comme le reste de la ligne.
func (s *serveur) composantModifier(w http.ResponseWriter, r *http.Request) {
	modeleID, errM := idChemin(r)
	revisionID, errR := idCheminNomme(r, "revisionID")
	compID, errC := idCheminNomme(r, "compID")
	if errM != nil || errR != nil || errC != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	quantite, errQ := strconv.ParseFloat(r.FormValue("quantite"), 64)
	capacite, errCap := strconv.ParseFloat(r.FormValue("capacite_unitaire"), 64)
	c := depot.Composant{
		ID: compID, RevisionID: revisionID,
		Nature: r.FormValue("nature"), Code: r.FormValue("code"),
		Quantite: quantite, CapaciteUnitaire: capacite,
		Unite: r.FormValue("unite"), Commentaire: chaineOptionnelleModele(r, "commentaire"),
	}
	var err error
	if errQ != nil || errCap != nil {
		err = fmt.Errorf("%w : quantité et capacité unitaire doivent être des nombres", depot.ErrValidation)
	} else {
		err = s.depotPour(r).ModifierComposant(c, corrigerFormulaire(r))
	}
	if err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreFragment(w, r, "composants_ligne_edition", composantLigne{Composant: c, ModeleID: modeleID, Erreur: messageUtilisateur(err)})
		return
	}

	composants, err := s.depot.ListerComposants(revisionID)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	relu, trouve := trouverComposant(composants, compID)
	if !trouve {
		s.repondreIntrouvable(w, r, fmt.Errorf("composant %d : %w", compID, depot.ErrIntrouvable))
		return
	}
	s.rendreFragment(w, r, "composants_ligne", composantLigne{Composant: relu, ModeleID: modeleID})
}

// composantSupprimer est l'action « supprimer » — pas d'étape d'édition
// préalable, donc la case « corriger » vit directement dans la ligne de
// lecture (composants_ligne) et est ramassée par hx-include="closest tr" sur
// ce bouton, comme sur celui de composants_ligne_edition.
func (s *serveur) composantSupprimer(w http.ResponseWriter, r *http.Request) {
	modeleID, errM := idChemin(r)
	revisionID, errR := idCheminNomme(r, "revisionID")
	compID, errC := idCheminNomme(r, "compID")
	if errM != nil || errR != nil || errC != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	err := s.depotPour(r).SupprimerComposant(compID, corrigerFormulaire(r))
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreComposantsTableau(w, r, modeleID, revisionID, messageUtilisateur(err))
}

// ---------------------------------------------------------------- nœuds par techno

// noeudsBloc est le contexte du gabarit "noeuds_tableau".
type noeudsBloc struct {
	ModeleID   int64
	RevisionID int64
	Noeuds     []noeudLigne
	Export     exportLiens // vers /modeles/{id}?tableau=noeuds&revision=…
}

// noeudLigne représente une ligne de la table des nœuds installables : une
// par techno active, valeur 0 si DefinirNoeuds n'a jamais été appelé pour ce
// couple (revision, techno) — pas de distinction lecture/édition, la ligne
// est toujours un formulaire (voir le commentaire en tête de
// templates/modeles/detail.html).
type noeudLigne struct {
	ModeleID      int64
	RevisionID    int64
	TechnoID      int64
	TechnoCode    string
	TechnoLibelle string
	NbNoeuds      int64
	Erreur        string
}

// noeudDefinir est la seule action des nœuds par techno : la case
// « corriger » et le champ nombre de nœuds sont ramassés ensemble par
// hx-include="closest tr" sur le bouton Enregistrer.
func (s *serveur) noeudDefinir(w http.ResponseWriter, r *http.Request) {
	modeleID, errM := idChemin(r)
	revisionID, errR := idCheminNomme(r, "revisionID")
	technoID, errT := idCheminNomme(r, "technoID")
	if errM != nil || errR != nil || errT != nil {
		http.Error(w, "identifiant invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	technos, err := s.depot.ListerTechnos(false)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	var techno depot.Techno
	for _, t := range technos {
		if t.ID == technoID {
			techno = t
			break
		}
	}

	nb, errNb := strconv.Atoi(r.FormValue("nb_noeuds"))
	var errEcriture error
	if errNb != nil {
		errEcriture = fmt.Errorf("%w : « %s » n'est pas un nombre de nœuds valide", depot.ErrValidation, r.FormValue("nb_noeuds"))
	} else {
		errEcriture = s.depotPour(r).DefinirNoeuds(revisionID, technoID, nb, corrigerFormulaire(r))
	}

	ligne := noeudLigne{
		ModeleID: modeleID, RevisionID: revisionID,
		TechnoID: technoID, TechnoCode: techno.Code, TechnoLibelle: techno.Libelle,
	}
	if errEcriture != nil {
		if !erreurMetier(errEcriture) {
			s.erreurServeur(w, r, errEcriture)
			return
		}
		// La saisie refusée ne doit pas s'afficher comme si elle avait été
		// appliquée (rule 5) : on relit la valeur persistée, 0 si elle n'a
		// jamais été définie pour ce couple (revision, techno).
		if actuel, errLecture := s.depot.LireNoeuds(revisionID, technoID); errLecture == nil {
			ligne.NbNoeuds = int64(actuel)
		}
		ligne.Erreur = messageUtilisateur(errEcriture)
		s.rendreFragment(w, r, "noeud_ligne", ligne)
		return
	}

	ligne.NbNoeuds = int64(nb)
	s.rendreFragment(w, r, "noeud_ligne", ligne)
}
