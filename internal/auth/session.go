package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// DureeSession est la durée de vie glissante d'une session : chaque activité
// la repousse d'autant. Choisie large (une journée de travail) pour un outil
// interne à faible effectif, où la contrainte de re-connexion fréquente
// n'apporte rien.
const DureeSession = 8 * time.Hour

// ErrSessionInvalide couvre un jeton absent, expiré, ou dont l'utilisateur a
// été désactivé depuis.
var ErrSessionInvalide = errors.New("session invalide ou expirée")

// Session est une session authentifiée, chargée depuis la table session.
type Session struct {
	Jeton         string
	UtilisateurID int64
	CSRFToken     string
	ExpireLe      time.Time
}

// GestionnaireSessions porte les sessions côté serveur : le jeton du cookie
// est opaque, révocable à tout instant (déconnexion, désactivation d'un
// compte), sans dépendre d'un secret de signature à faire tourner.
//
// Ce n'est pas un dépôt internal/depot au sens du domaine métier : la session
// est un mécanisme d'authentification, pas une entité de
// docs/modele-donnees.md. D'où l'accès direct à *sql.DB plutôt qu'un passage
// par internal/depot.
type GestionnaireSessions struct {
	base *sql.DB
}

// NouveauGestionnaireSessions construit le gestionnaire sur la base ouverte
// (migrations déjà appliquées, table session comprise).
func NouveauGestionnaireSessions(base *sql.DB) *GestionnaireSessions {
	return &GestionnaireSessions{base: base}
}

// Ouvrir crée une session pour l'utilisateur authentifié et renvoie son jeton
// et son jeton CSRF, à poser respectivement en cookie HttpOnly et à
// distribuer aux gabarits.
func (g *GestionnaireSessions) Ouvrir(utilisateurID int64) (Session, error) {
	jeton, err := jetonAleatoire()
	if err != nil {
		return Session{}, err
	}
	csrf, err := jetonAleatoire()
	if err != nil {
		return Session{}, err
	}

	maintenant := time.Now().UTC()
	expire := maintenant.Add(DureeSession)
	_, err = g.base.Exec(
		`INSERT INTO session (id, utilisateur_id, csrf_token, cree_le, expire_le, derniere_activite)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		jeton, utilisateurID, csrf, iso(maintenant), iso(expire), iso(maintenant))
	if err != nil {
		return Session{}, fmt.Errorf("ouverture de session : %w", err)
	}
	return Session{Jeton: jeton, UtilisateurID: utilisateurID, CSRFToken: csrf, ExpireLe: expire}, nil
}

// Charger renvoie la session correspondant au jeton si elle est valide (non
// expirée) et prolonge sa durée de vie glissante d'une DureeSession
// supplémentaire.
//
// ErrSessionInvalide couvre l'absence, l'expiration, ou un utilisateur
// entretemps désactivé — l'appelant n'a pas à distinguer ces cas, tous
// se traduisent par une reconnexion demandée.
func (g *GestionnaireSessions) Charger(jeton string) (Session, error) {
	if jeton == "" {
		return Session{}, ErrSessionInvalide
	}
	var s Session
	var expireStr string
	err := g.base.QueryRow(
		`SELECT sess.id, sess.utilisateur_id, sess.csrf_token, sess.expire_le
		 FROM session sess
		 JOIN utilisateur u ON u.id = sess.utilisateur_id
		 WHERE sess.id = ? AND u.actif = 1`, jeton).
		Scan(&s.Jeton, &s.UtilisateurID, &s.CSRFToken, &expireStr)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrSessionInvalide
	}
	if err != nil {
		return Session{}, fmt.Errorf("lecture de session : %w", err)
	}
	expire, err := time.Parse(time.RFC3339, expireStr)
	if err != nil {
		return Session{}, fmt.Errorf("lecture de session : date d'expiration illisible : %w", err)
	}
	maintenant := time.Now().UTC()
	if maintenant.After(expire) {
		_ = g.Invalider(jeton)
		return Session{}, ErrSessionInvalide
	}
	s.ExpireLe = expire

	// prolongation glissante : n'écrire que si ça vaut la peine, pour ne pas
	// taper la base à chaque requête d'un utilisateur déjà à jour
	if expire.Sub(maintenant) < DureeSession-time.Minute {
		nouvelleExpiration := maintenant.Add(DureeSession)
		if _, err := g.base.Exec(
			`UPDATE session SET expire_le = ?, derniere_activite = ? WHERE id = ?`,
			iso(nouvelleExpiration), iso(maintenant), jeton); err == nil {
			s.ExpireLe = nouvelleExpiration
		}
	}
	return s, nil
}

// Invalider supprime une session : c'est la déconnexion. Ne renvoie pas
// d'erreur si le jeton n'existait déjà plus.
func (g *GestionnaireSessions) Invalider(jeton string) error {
	_, err := g.base.Exec(`DELETE FROM session WHERE id = ?`, jeton)
	if err != nil {
		return fmt.Errorf("invalidation de session : %w", err)
	}
	return nil
}

// Purger retire les sessions expirées. À appeler périodiquement (pas de
// dépendance à un ordonnanceur externe : un simple ticker dans main suffit).
func (g *GestionnaireSessions) Purger() error {
	_, err := g.base.Exec(`DELETE FROM session WHERE expire_le < ?`, iso(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("purge des sessions : %w", err)
	}
	return nil
}

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func jetonAleatoire() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("génération de jeton : %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
