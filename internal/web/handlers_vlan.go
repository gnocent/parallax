package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"parallax/internal/depot"
)

// Écran Catalogue des VLAN (backlog v3.1) : code, critères d'application
// (projet, environnement, zone, cluster — vide = tous), plages début–fin,
// curseur d'attribution et commentaire. Réplique le patron de référence
// (handlers_projet.go) avec deux différences :
//
//   - la suppression existe (un catalogue se corrige, il n'a pas
//     d'historique à préserver) ; elle est refusée tant qu'un serveur porte
//     le VLAN ;
//   - les plages se gèrent dans la ligne du VLAN (ajout / suppression),
//     et chaque mutation de plage renvoie le tableau entier : la ligne
//     change de hauteur et un message d'erreur (chevauchement, borne
//     invalide) doit s'afficher au même endroit que pour la création.
//
// routesVlans() n'est pas appelée depuis server.go : l'intégration ajoute
// cet appel (voir la doc de tête de middleware.go).
//
//	GET    /vlans                          page complète (création + tableau)
//	POST   /vlans                          créer, répond le tableau entier
//	GET    /vlans/{id}                     ligne en lecture (fragment)
//	GET    /vlans/{id}/editer              ligne en édition (fragment)
//	PUT    /vlans/{id}                     modifier, répond la ligne
//	DELETE /vlans/{id}                     supprimer, répond le tableau entier
//	POST   /vlans/{id}/plages              ajouter une plage, répond le tableau
//	DELETE /vlans/{id}/plages/{plage}      retirer une plage, répond le tableau
func (s *serveur) routesVlans() {
	s.mux.HandleFunc("GET /vlans", s.lecteur(s.vlansPage))
	s.mux.HandleFunc("POST /vlans", s.editeur(s.vlansCreer))
	s.mux.HandleFunc("GET /vlans/{id}", s.lecteur(s.vlansLigne))
	s.mux.HandleFunc("GET /vlans/{id}/editer", s.editeur(s.vlansFormulaireEdition))
	s.mux.HandleFunc("PUT /vlans/{id}", s.editeur(s.vlansModifier))
	s.mux.HandleFunc("DELETE /vlans/{id}", s.editeur(s.vlansSupprimer))
	s.mux.HandleFunc("POST /vlans/{id}/plages", s.editeur(s.vlansPlageAjouter))
	s.mux.HandleFunc("DELETE /vlans/{id}/plages/{plage}", s.editeur(s.vlansPlageSupprimer))
}

// optionsVlan alimente les <select> des critères : référentiels complets
// (archivés compris, un VLAN peut viser un projet en sommeil) et clusters.
type optionsVlan struct {
	Projets        []depot.Projet
	Environnements []depot.Environnement
	Zones          []depot.Zone
	Clusters       []optionCluster
}

// optionCluster est un cluster présenté avec son projet et son
// environnement : deux clusters homonymes (ElasticHot PROD et PREPROD) se
// distinguent ainsi dans la liste.
type optionCluster struct {
	ID      int64
	Libelle string
}

// vlanLigne est le contexte des gabarits vlans_ligne et
// vlans_ligne_edition : le VLAN, les libellés résolus de ses critères, les
// identifiants sélectionnés (0 = tous, pour {{eq}} dans les <select>), un
// message d'erreur optionnel et les options des listes.
type vlanLigne struct {
	depot.Vlan
	Projet, Environnement, Zone, Cluster   string
	ProjetSel, EnvSel, ZoneSel, ClusterSel int64
	Erreur                                 string
	Options                                *optionsVlan
}

// libellesVlan regroupe les dictionnaires id -> libellé des critères.
type libellesVlan struct {
	projets, environnements, zones, clusters map[int64]string
}

func (s *serveur) chargerOptionsVlan() (*optionsVlan, libellesVlan, error) {
	lib := libellesVlan{
		projets: map[int64]string{}, environnements: map[int64]string{},
		zones: map[int64]string{}, clusters: map[int64]string{},
	}
	projets, err := s.depot.ListerProjets(true)
	if err != nil {
		return nil, lib, err
	}
	envs, err := s.depot.ListerEnvironnements()
	if err != nil {
		return nil, lib, err
	}
	zones, err := s.depot.ListerZones()
	if err != nil {
		return nil, lib, err
	}
	clusters, err := s.depot.ListerClusters(depot.FiltreCluster{InclureInactifs: true})
	if err != nil {
		return nil, lib, err
	}
	opt := &optionsVlan{Projets: projets, Environnements: envs, Zones: zones}
	for _, p := range projets {
		lib.projets[p.ID] = p.Code
	}
	for _, e := range envs {
		lib.environnements[e.ID] = e.Code
	}
	for _, z := range zones {
		lib.zones[z.ID] = z.Code
	}
	for _, c := range clusters {
		libelle := fmt.Sprintf("%s (%s / %s)", c.Nom, lib.projets[c.ProjetID], lib.environnements[c.EnvironnementID])
		lib.clusters[c.ID] = libelle
		opt.Clusters = append(opt.Clusters, optionCluster{ID: c.ID, Libelle: libelle})
	}
	return opt, lib, nil
}

func enLigneVlan(v depot.Vlan, lib libellesVlan, opt *optionsVlan) vlanLigne {
	l := vlanLigne{Vlan: v, Options: opt}
	if v.ProjetID != nil {
		l.ProjetSel, l.Projet = *v.ProjetID, lib.projets[*v.ProjetID]
	}
	if v.EnvironnementID != nil {
		l.EnvSel, l.Environnement = *v.EnvironnementID, lib.environnements[*v.EnvironnementID]
	}
	if v.ZoneID != nil {
		l.ZoneSel, l.Zone = *v.ZoneID, lib.zones[*v.ZoneID]
	}
	if v.ClusterID != nil {
		l.ClusterSel, l.Cluster = *v.ClusterID, lib.clusters[*v.ClusterID]
	}
	return l
}

// donneesTableauVlans construit le contexte du gabarit vlans_tableau (et de
// la page) : toutes les lignes avec leurs plages, les options et les liens
// d'export. erreur est le message métier à afficher en tête, vide sinon.
func (s *serveur) donneesTableauVlans(r *http.Request, erreur string) (map[string]any, error) {
	vlans, err := s.depot.ListerVlans()
	if err != nil {
		return nil, err
	}
	opt, lib, err := s.chargerOptionsVlan()
	if err != nil {
		return nil, err
	}
	lignes := make([]vlanLigne, len(vlans))
	for i, v := range vlans {
		lignes[i] = enLigneVlan(v, lib, opt)
	}
	return map[string]any{
		"Vlans": lignes, "Erreur": erreur,
		// Nouveau : contexte vide (aucun critère sélectionné) pour les
		// <select> du formulaire de création, mêmes gabarits que l'édition.
		"Nouveau": vlanLigne{Options: opt},
		"Export":  liensExportVers(r, "/vlans", ""),
	}, nil
}

func (s *serveur) vlansPage(w http.ResponseWriter, r *http.Request) {
	donnees, err := s.donneesTableauVlans(r, "")
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "VLAN", Colonnes: []string{
			"Code", "Projet", "Environnement", "Zone", "Cluster", "Plages", "Curseur", "Commentaire"}}
		for _, l := range donnees["Vlans"].([]vlanLigne) {
			t.Lignes = append(t.Lignes, []any{
				l.Code, l.Projet, l.Environnement, l.Zone, l.Cluster,
				l.PlagesTexte(), texteOuVide(l.DerniereIP), texteOuVide(l.Commentaire),
			})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.vlan"), "vlans_page", donnees)
}

// repondreTableauVlans renvoie le tableau entier avec un éventuel message
// métier — la réponse commune de la création, de la suppression et des
// mutations de plages.
func (s *serveur) repondreTableauVlans(w http.ResponseWriter, r *http.Request, errMetier error) {
	if errMetier != nil && !erreurMetier(errMetier) {
		s.erreurServeur(w, r, errMetier)
		return
	}
	donnees, err := s.donneesTableauVlans(r, messageUtilisateur(errMetier))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "vlans_tableau", donnees)
}

// lireVlanFormulaire lit les champs communs à la création et à la
// modification. Une erreur de parsing d'identifiant est une erreur métier
// affichable (le <select> n'envoie jamais ça, sauf requête forgée).
func lireVlanFormulaire(r *http.Request) (depot.Vlan, error) {
	v := depot.Vlan{
		Code:        r.FormValue("code"),
		DerniereIP:  texteOptionnel(r, "derniere_ip"),
		Commentaire: texteOptionnel(r, "commentaire"),
	}
	var err error
	for _, champ := range []struct {
		nom   string
		cible **int64
	}{
		{"projet_id", &v.ProjetID}, {"environnement_id", &v.EnvironnementID},
		{"zone_id", &v.ZoneID}, {"cluster_id", &v.ClusterID},
	} {
		if *champ.cible, err = idOptionnel(r, champ.nom); err != nil {
			return v, fmt.Errorf("%w : %v", depot.ErrValidation, err)
		}
	}
	return v, nil
}

func (s *serveur) vlansCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	v, err := lireVlanFormulaire(r)
	if err == nil {
		_, err = s.depotPour(r).CreerVlan(v)
	}
	s.repondreTableauVlans(w, r, err)
}

func (s *serveur) vlansLigne(w http.ResponseWriter, r *http.Request) {
	s.vlansRendreLigne(w, r, "vlans_ligne", "")
}

func (s *serveur) vlansFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	s.vlansRendreLigne(w, r, "vlans_ligne_edition", "")
}

func (s *serveur) vlansRendreLigne(w http.ResponseWriter, r *http.Request, gabarit, erreur string) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	v, err := s.depot.LireVlan(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	opt, lib, err := s.chargerOptionsVlan()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	l := enLigneVlan(v, lib, opt)
	l.Erreur = erreur
	s.rendreFragment(w, r, gabarit, l)
}

func (s *serveur) vlansModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	v, err := lireVlanFormulaire(r)
	if err == nil {
		v.ID = id
		err = s.depotPour(r).ModifierVlan(v)
	}
	if err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		// on réaffiche la saisie avec l'erreur ; les plages viennent de la
		// base (la modification ne les touche pas).
		actuel, errLecture := s.depot.LireVlan(id)
		if errLecture != nil {
			s.repondreIntrouvable(w, r, errLecture)
			return
		}
		opt, lib, errOpt := s.chargerOptionsVlan()
		if errOpt != nil {
			s.erreurServeur(w, r, errOpt)
			return
		}
		v.Plages = actuel.Plages
		l := enLigneVlan(v, lib, opt)
		l.Erreur = messageUtilisateur(err)
		s.rendreFragment(w, r, "vlans_ligne_edition", l)
		return
	}
	s.vlansRendreLigne(w, r, "vlans_ligne", "")
}

func (s *serveur) vlansSupprimer(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = s.depotPour(r).SupprimerVlan(id)
	if errors.Is(err, depot.ErrReference) {
		// un serveur porte encore ce VLAN — message dédié, celui du dépôt
		// parle de clé étrangère.
		err = fmt.Errorf("%w : ce VLAN est encore porté par au moins un serveur ; le retirer des serveurs avant de le supprimer", depot.ErrValidation)
	}
	s.repondreTableauVlans(w, r, err)
}

func (s *serveur) vlansPlageAjouter(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	_, err = s.depotPour(r).CreerPlage(depot.PlageIP{
		VlanID: id, IPDebut: r.FormValue("ip_debut"), IPFin: r.FormValue("ip_fin"),
	})
	s.repondreTableauVlans(w, r, err)
}

func (s *serveur) vlansPlageSupprimer(w http.ResponseWriter, r *http.Request) {
	plageID, err := strconv.ParseInt(r.PathValue("plage"), 10, 64)
	if err != nil {
		http.Error(w, "identifiant de plage invalide dans l'adresse", http.StatusBadRequest)
		return
	}
	s.repondreTableauVlans(w, r, s.depotPour(r).SupprimerPlage(plageID))
}
