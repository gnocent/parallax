package auth

import (
	"errors"
	"fmt"

	"parallax/internal/depot"
)

// Erreurs renvoyées par un Authenticator. Volontairement indifférenciées côté
// utilisateur (mauvais login et mauvais mot de passe renvoient la même
// ErrIdentifiantsInvalides) pour ne pas révéler quels logins existent.
var (
	ErrIdentifiantsInvalides = errors.New("identifiants invalides")
	ErrCompteInactif         = errors.New("compte désactivé")
)

// Identite est ce qu'un Authenticator garantit après vérification réussie.
type Identite struct {
	UtilisateurID int64
	Login         string
	Nom           *string
	Role          string
}

// Authenticator vérifie une paire login/mot de passe et renvoie l'identité
// correspondante. Une seule implémentation existe aujourd'hui
// (LocalAuthenticator, comptes locaux + argon2id) ; l'interface est le point
// d'extension pour LDAP ou SSO plus tard, sans toucher au reste de
// l'application.
type Authenticator interface {
	Authentifier(login, motDePasse string) (Identite, error)
}

// LocalAuthenticator authentifie contre les comptes locaux stockés par
// internal/depot.
type LocalAuthenticator struct {
	depot *depot.Depot
}

// NouvelAuthenticatorLocal construit un LocalAuthenticator sur le dépôt donné.
func NouvelAuthenticatorLocal(d *depot.Depot) *LocalAuthenticator {
	return &LocalAuthenticator{depot: d}
}

// Authentifier vérifie le mot de passe d'un compte local actif.
//
// Erreurs : ErrIdentifiantsInvalides si le login est inconnu ou le mot de
// passe erroné ; ErrCompteInactif si le compte existe mais est désactivé.
func (a *LocalAuthenticator) Authentifier(login, motDePasse string) (Identite, error) {
	u, err := a.depot.LireUtilisateurParLogin(login)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			return Identite{}, ErrIdentifiantsInvalides
		}
		return Identite{}, fmt.Errorf("authentification : %w", err)
	}
	if !u.Actif {
		return Identite{}, ErrCompteInactif
	}

	ok, err := VerifierMotDePasse(u.Hash, motDePasse)
	if err != nil {
		return Identite{}, fmt.Errorf("authentification : %w", err)
	}
	if !ok {
		return Identite{}, ErrIdentifiantsInvalides
	}

	return Identite{UtilisateurID: u.ID, Login: u.Login, Nom: u.Nom, Role: u.Role}, nil
}
