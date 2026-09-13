package auth

import (
	"errors"
	"fmt"

	"parallax/internal/depot"
	"parallax/internal/ldap"
)

// AuthenticatorLDAP (backlog v3.6) combine les comptes locaux et l'annuaire :
//
//   - le login désigne un compte LOCAL → argon2id, comme avant ;
//   - il désigne un compte LDAP, ou aucun compte → bind simple sur
//     l'annuaire avec le mot de passe saisi, jamais stocké ;
//   - un compte désactivé est refusé quel que soit le résultat du bind ;
//   - un premier bind réussi crée le compte en LECTEUR (journalisé) ; un
//     administrateur le promeut ensuite. Aucune désactivation automatique.
//
// Les comptes locaux restent le secours : l'annuaire injoignable, un
// administrateur local se connecte toujours.
type AuthenticatorLDAP struct {
	depot *depot.Depot
	local *LocalAuthenticator
	lieur ldap.Lieur
}

// NouvelAuthenticatorLDAP construit l'authentificateur combiné. lieur est
// le client réel en production, un faux dans les tests.
func NouvelAuthenticatorLDAP(d *depot.Depot, lieur ldap.Lieur) *AuthenticatorLDAP {
	return &AuthenticatorLDAP{depot: d, local: NouvelAuthenticatorLocal(d), lieur: lieur}
}

// Authentifier applique la règle ci-dessus.
//
// Erreurs : ErrIdentifiantsInvalides (refus local ou de l'annuaire, login
// vide) ; ErrCompteInactif ; toute panne (annuaire injoignable, base) est
// remontée telle quelle, enveloppée — l'écran de connexion la montre comme
// une erreur technique, pas comme un mauvais mot de passe.
func (a *AuthenticatorLDAP) Authentifier(login, motDePasse string) (Identite, error) {
	u, err := a.depot.LireUtilisateurParLogin(login)
	switch {
	case err == nil:
		if !u.Actif {
			return Identite{}, ErrCompteInactif
		}
		if u.Origine == depot.OrigineLocale {
			return a.local.Authentifier(login, motDePasse)
		}
		if err := a.lier(login, motDePasse); err != nil {
			return Identite{}, err
		}
		return Identite{UtilisateurID: u.ID, Login: u.Login, Nom: u.Nom, Role: u.Role}, nil
	case errors.Is(err, depot.ErrIntrouvable):
		if err := a.lier(login, motDePasse); err != nil {
			return Identite{}, err
		}
		cree, err := a.depot.CreerUtilisateurLDAP(login)
		if err != nil {
			return Identite{}, fmt.Errorf("création du compte LDAP : %w", err)
		}
		return Identite{UtilisateurID: cree.ID, Login: cree.Login, Nom: cree.Nom, Role: cree.Role}, nil
	default:
		return Identite{}, fmt.Errorf("authentification : %w", err)
	}
}

func (a *AuthenticatorLDAP) lier(login, motDePasse string) error {
	err := a.lieur.Lier(login, motDePasse)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ldap.ErrRefus):
		return ErrIdentifiantsInvalides
	default:
		return fmt.Errorf("annuaire LDAP : %w", err)
	}
}
