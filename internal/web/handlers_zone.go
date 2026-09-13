package web

import (
	"net/http"

	"parallax/internal/depot"
)

// routesZones réplique le patron de référence (voir
// handlers_projet.go), simplifié comme Usage : pas de drapeau d'activité.
// Champ Site en plus, facultatif (*string).
func (s *serveur) routesZones() {
	s.mux.HandleFunc("GET /zones", s.lecteur(s.zonesPage))
	s.mux.HandleFunc("GET /zones/tableau", s.lecteur(s.zonesTableau))
	s.mux.HandleFunc("POST /zones", s.editeur(s.zonesCreer))
	s.mux.HandleFunc("GET /zones/{id}", s.lecteur(s.zonesLigne))
	s.mux.HandleFunc("GET /zones/{id}/editer", s.editeur(s.zonesFormulaireEdition))
	s.mux.HandleFunc("PUT /zones/{id}", s.editeur(s.zonesModifier))
}

// zoneLigne incorpore depot.Zone et ajoute un message d'erreur
// optionnel — voir le commentaire de projetLigne dans handlers_projet.go,
// même raison.
type zoneLigne struct {
	depot.Zone
	Erreur string
}

func zonesEnLignes(zones []depot.Zone) []zoneLigne {
	out := make([]zoneLigne, len(zones))
	for i, dc := range zones {
		out[i] = zoneLigne{Zone: dc}
	}
	return out
}

// siteFormulaire lit le champ "site" du formulaire soumis et renvoie nil si
// absent ou vide : le dépôt normaliserait de toute façon une chaîne vide en
// NULL, mais on ne veut pas envoyer une chaîne vide là où l'absence de valeur
// est le cas attendu — voir depot.normaliserSite.
func siteFormulaire(r *http.Request) *string {
	v := r.FormValue("site")
	if v == "" {
		return nil
	}
	return &v
}

func (s *serveur) zonesPage(w http.ResponseWriter, r *http.Request) {
	zones, err := s.depot.ListerZones()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : mêmes données que la page.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Zones", Colonnes: []string{"Code", "Libellé", "Site"}}
		for _, z := range zones {
			t.Lignes = append(t.Lignes, []any{z.Code, z.Libelle, texteOuVide(z.Site)})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.zones"), "zones_page", map[string]any{
		"Zones": zonesEnLignes(zones), "Export": exportZones(r),
	})
}

// exportZones : liens d'export posés dans le fragment, vers la page (voir
// exportProjets). Pas de filtre sur cet écran.
func exportZones(r *http.Request) exportLiens {
	return liensExportVers(r, "/zones", "")
}

func (s *serveur) zonesTableau(w http.ResponseWriter, r *http.Request) {
	zones, err := s.depot.ListerZones()
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "zones_tableau", map[string]any{
		"Zones": zonesEnLignes(zones), "Export": exportZones(r),
	})
}

// zonesCreer renvoie toujours le tableau entier — voir projetsCreer.
func (s *serveur) zonesCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	_, err := s.depotPour(r).CreerZone(depot.Zone{
		Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Site: siteFormulaire(r),
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}

	zones, errListe := s.depot.ListerZones()
	if errListe != nil {
		s.erreurServeur(w, r, errListe)
		return
	}
	s.rendreFragment(w, r, "zones_tableau", map[string]any{
		"Zones": zonesEnLignes(zones), "Erreur": messageUtilisateur(err), "Export": exportZones(r),
	})
}

func (s *serveur) zonesLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dc, err := s.depot.LireZone(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "zones_ligne", zoneLigne{Zone: dc})
}

func (s *serveur) zonesFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dc, err := s.depot.LireZone(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "zones_ligne_edition", zoneLigne{Zone: dc})
}

func (s *serveur) zonesModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	dc := depot.Zone{ID: id, Code: r.FormValue("code"), Libelle: r.FormValue("libelle"), Site: siteFormulaire(r)}
	if err := s.depotPour(r).ModifierZone(dc); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreFragment(w, r, "zones_ligne_edition", zoneLigne{Zone: dc, Erreur: messageUtilisateur(err)})
		return
	}
	relu, err := s.depot.LireZone(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "zones_ligne", zoneLigne{Zone: relu})
}
