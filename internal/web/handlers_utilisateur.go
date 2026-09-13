package web

import (
	"net/http"

	"parallax/internal/auth"
	"parallax/internal/depot"
)

// routesUtilisateurs gère les comptes locaux — réservé au rôle ADMIN
// . Patron d'écran identique à routesProjets, avec deux
// particularités : un mot de passe initial à la création (haché avant
// stockage, jamais conservé en clair), et une réinitialisation de mot de
// passe séparée de l'édition normale (nom, rôle) — deux actions, deux
// intentions, comme pour la correction d'une révision.
func (s *serveur) routesUtilisateurs() {
	s.mux.HandleFunc("GET /utilisateurs", s.administrateur(s.utilisateursPage))
	s.mux.HandleFunc("GET /utilisateurs/tableau", s.administrateur(s.utilisateursTableau))
	s.mux.HandleFunc("POST /utilisateurs", s.administrateur(s.utilisateursCreer))
	s.mux.HandleFunc("GET /utilisateurs/{id}", s.administrateur(s.utilisateursLigne))
	s.mux.HandleFunc("GET /utilisateurs/{id}/editer", s.administrateur(s.utilisateursFormulaireEdition))
	s.mux.HandleFunc("PUT /utilisateurs/{id}", s.administrateur(s.utilisateursModifier))
	s.mux.HandleFunc("POST /utilisateurs/{id}/archiver", s.administrateur(s.utilisateursArchiver))
	s.mux.HandleFunc("POST /utilisateurs/{id}/reactiver", s.administrateur(s.utilisateursReactiver))
	s.mux.HandleFunc("GET /utilisateurs/{id}/mot-de-passe", s.administrateur(s.utilisateursFormulaireMotDePasse))
	s.mux.HandleFunc("PUT /utilisateurs/{id}/mot-de-passe", s.administrateur(s.utilisateursReinitialiserMotDePasse))
}

type utilisateurLigne struct {
	depot.Utilisateur
	Erreur string
}

func utilisateursEnLignes(us []depot.Utilisateur) []utilisateurLigne {
	out := make([]utilisateurLigne, len(us))
	for i, u := range us {
		out[i] = utilisateurLigne{Utilisateur: u}
	}
	return out
}

func (s *serveur) utilisateursPage(w http.ResponseWriter, r *http.Request) {
	us, err := s.depot.ListerUtilisateurs(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	// export universel (tableau.go) : identifiant, nom, rôle, activité — jamais
	// le hash, qui ne quitte pas la base.
	if s.exporter(w, r, func() (tableau, error) {
		t := tableau{Titre: "Comptes", Colonnes: []string{"Identifiant", "Nom", "Rôle", "Actif", "Origine"}}
		for _, u := range us {
			t.Lignes = append(t.Lignes, []any{u.Login, texteOuVide(u.Nom), u.Role, u.Actif, u.Origine})
		}
		return t, nil
	}) {
		return
	}
	s.rendrePage(w, r, s.titre(r, "titre.comptes"), "utilisateurs_page", map[string]any{
		"Utilisateurs": utilisateursEnLignes(us),
		"Roles":        []string{depot.RoleLecteur, depot.RoleEditeur, depot.RoleAdmin},
		"Export":       exportUtilisateurs(r),
	})
}

// exportUtilisateurs : liens d'export posés dans le fragment, vers la page,
// avec l'état de la case « inclure les désactivés » (voir exportProjets).
func exportUtilisateurs(r *http.Request) exportLiens {
	return liensExportVers(r, "/utilisateurs", "", "inclure_inactifs")
}

func (s *serveur) utilisateursTableau(w http.ResponseWriter, r *http.Request) {
	us, err := s.depot.ListerUtilisateurs(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_tableau", map[string]any{
		"Utilisateurs": utilisateursEnLignes(us), "Export": exportUtilisateurs(r),
	})
}

func (s *serveur) utilisateursCreer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}

	hash, err := auth.HacherMotDePasse(r.FormValue("mot_de_passe"))
	if err != nil {
		// mot de passe vide ou hachage impossible : erreur métier, pas
		// serveur — l'utilisateur peut corriger sa saisie.
		s.reafficherTableauUtilisateurs(w, r, "Mot de passe invalide : "+err.Error())
		return
	}

	role := r.FormValue("role")
	nom := (*string)(nil)
	if v := r.FormValue("nom"); v != "" {
		nom = &v
	}
	_, err = s.depotPour(r).CreerUtilisateur(depot.Utilisateur{
		Login: r.FormValue("login"), Hash: hash, Nom: nom, Role: role,
	})
	if err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	s.reafficherTableauUtilisateurs(w, r, messageUtilisateur(err))
}

func (s *serveur) reafficherTableauUtilisateurs(w http.ResponseWriter, r *http.Request, erreur string) {
	us, err := s.depot.ListerUtilisateurs(inclureInactifs(r))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_tableau", map[string]any{
		"Utilisateurs": utilisateursEnLignes(us), "Erreur": erreur, "Export": exportUtilisateurs(r),
	})
}

func (s *serveur) utilisateursLigne(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_ligne", utilisateurLigne{Utilisateur: u})
}

func (s *serveur) utilisateursFormulaireEdition(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_ligne_edition", map[string]any{
		"U":     utilisateurLigne{Utilisateur: u},
		"Roles": []string{depot.RoleLecteur, depot.RoleEditeur, depot.RoleAdmin},
	})
}

func (s *serveur) utilisateursModifier(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	actuel, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}

	nom := (*string)(nil)
	if v := r.FormValue("nom"); v != "" {
		nom = &v
	}
	actuel.Nom = nom
	actuel.Role = r.FormValue("role")

	if err := s.depotPour(r).ModifierUtilisateur(actuel); err != nil {
		if !erreurMetier(err) {
			s.erreurServeur(w, r, err)
			return
		}
		s.rendreFragment(w, r, "utilisateurs_ligne_edition", map[string]any{
			"U":     utilisateurLigne{Utilisateur: actuel, Erreur: messageUtilisateur(err)},
			"Roles": []string{depot.RoleLecteur, depot.RoleEditeur, depot.RoleAdmin},
		})
		return
	}
	relu, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_ligne", utilisateurLigne{Utilisateur: relu})
}

func (s *serveur) utilisateursArchiver(w http.ResponseWriter, r *http.Request) {
	s.utilisateursBasculerActivite(w, r, s.depotPour(r).ArchiverUtilisateur)
}

func (s *serveur) utilisateursReactiver(w http.ResponseWriter, r *http.Request) {
	s.utilisateursBasculerActivite(w, r, s.depotPour(r).ReactiverUtilisateur)
}

func (s *serveur) utilisateursBasculerActivite(w http.ResponseWriter, r *http.Request, action func(int64) error) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := action(id); err != nil && !erreurMetier(err) {
		s.erreurServeur(w, r, err)
		return
	}
	u, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_ligne", utilisateurLigne{Utilisateur: u})
}

func (s *serveur) utilisateursFormulaireMotDePasse(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_ligne_mot_de_passe", utilisateurLigne{Utilisateur: u})
}

func (s *serveur) utilisateursReinitialiserMotDePasse(w http.ResponseWriter, r *http.Request) {
	id, err := idChemin(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := s.depot.LireUtilisateur(id)
	if err != nil {
		s.repondreIntrouvable(w, r, err)
		return
	}
	if u.Origine == depot.OrigineLDAP {
		// le mot de passe d'un compte LDAP appartient à l'annuaire (v3.6).
		s.rendreFragment(w, r, "utilisateurs_ligne",
			utilisateurLigne{Utilisateur: u, Erreur: "Compte LDAP : le mot de passe est celui de l'annuaire, il ne se réinitialise pas ici."})
		return
	}

	hash, err := auth.HacherMotDePasse(r.FormValue("mot_de_passe"))
	if err != nil {
		s.rendreFragment(w, r, "utilisateurs_ligne_mot_de_passe",
			utilisateurLigne{Utilisateur: u, Erreur: "Mot de passe invalide : " + err.Error()})
		return
	}
	if err := s.depotPour(r).DefinirHash(id, hash); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "utilisateurs_ligne", utilisateurLigne{Utilisateur: u})
}
