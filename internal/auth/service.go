package auth

import (
	"fmt"

	"parallax/internal/depot"
)

// Service assemble l'authentification et les sessions : c'est ce que la
// couche web appelle. Les rôles utilisés pour l'autorisation sont ceux
// déclarés par internal/depot (depot.RoleAdmin, depot.RoleEditeur,
// depot.RoleLecteur) — une seule source pour l'énumération.
type Service struct {
	Auth     Authenticator
	Sessions *GestionnaireSessions
	depot    *depot.Depot
}

// NouveauService construit le service d'authentification local : comptes de
// internal/depot, sessions dans la même base.
func NouveauService(d *depot.Depot) *Service {
	return &Service{
		Auth:     NouvelAuthenticatorLocal(d),
		Sessions: NouveauGestionnaireSessions(d.Base()),
		depot:    d,
	}
}

// Connecter vérifie les identifiants et ouvre une session.
//
// Erreurs : ErrIdentifiantsInvalides, ErrCompteInactif (voir Authenticator).
func (s *Service) Connecter(login, motDePasse string) (Session, Identite, error) {
	identite, err := s.Auth.Authentifier(login, motDePasse)
	if err != nil {
		return Session{}, Identite{}, err
	}
	session, err := s.Sessions.Ouvrir(identite.UtilisateurID)
	if err != nil {
		return Session{}, Identite{}, err
	}
	return session, identite, nil
}

// Deconnecter invalide la session portant ce jeton.
func (s *Service) Deconnecter(jeton string) error {
	return s.Sessions.Invalider(jeton)
}

// Courant charge la session d'un jeton de cookie et l'identité associée.
// L'identité est relue à chaque appel (pas mise en cache dans la session) :
// un changement de rôle prend effet à la requête suivante, pas seulement à
// la prochaine connexion.
//
// Erreurs : ErrSessionInvalide (jeton absent, expiré, ou compte désactivé
// entretemps).
func (s *Service) Courant(jeton string) (Session, Identite, error) {
	session, err := s.Sessions.Charger(jeton)
	if err != nil {
		return Session{}, Identite{}, err
	}
	u, err := s.depot.LireUtilisateur(session.UtilisateurID)
	if err != nil {
		return Session{}, Identite{}, fmt.Errorf("identité de session : %w", err)
	}
	return session, Identite{UtilisateurID: u.ID, Login: u.Login, Nom: u.Nom, Role: u.Role}, nil
}
