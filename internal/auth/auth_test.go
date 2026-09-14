package auth

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"parallax/internal/db"
	"parallax/internal/depot"
)

func depotDeTest(t *testing.T) *depot.Depot {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "auth-test.db")
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatalf("ouverture de la base de test : %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	return depot.Nouveau(base)
}

func utilisateurDeTest(t *testing.T, d *depot.Depot, login, motDePasse, role string) depot.Utilisateur {
	t.Helper()
	hash, err := HacherMotDePasse(motDePasse)
	if err != nil {
		t.Fatalf("hachage : %v", err)
	}
	u, err := d.CreerUtilisateur(depot.Utilisateur{Login: login, Hash: hash, Role: role})
	if err != nil {
		t.Fatalf("création utilisateur : %v", err)
	}
	return u
}

func TestHacherEtVerifierMotDePasse(t *testing.T) {
	hash, err := HacherMotDePasse("correct horse battery staple")
	if err != nil {
		t.Fatalf("hachage : %v", err)
	}
	ok, err := VerifierMotDePasse(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("le bon mot de passe doit être accepté : ok=%v err=%v", ok, err)
	}
	ok, err = VerifierMotDePasse(hash, "mauvais mot de passe")
	if err != nil || ok {
		t.Fatalf("un mauvais mot de passe doit être rejeté sans erreur : ok=%v err=%v", ok, err)
	}
	// deux hachages du même mot de passe diffèrent (sel aléatoire)
	autre, _ := HacherMotDePasse("correct horse battery staple")
	if autre == hash {
		t.Fatal("le sel doit varier d'un hachage à l'autre")
	}
}

func TestAuthentifierLocal(t *testing.T) {
	d := depotDeTest(t)
	utilisateurDeTest(t, d, "editeur", "s3cret!", depot.RoleAdmin)

	a := NouvelAuthenticatorLocal(d)

	id, err := a.Authentifier("editeur", "s3cret!")
	if err != nil {
		t.Fatalf("authentification valide refusée : %v", err)
	}
	if id.Role != depot.RoleAdmin || id.Login != "editeur" {
		t.Fatalf("identité inattendue : %+v", id)
	}

	if _, err := a.Authentifier("editeur", "mauvais"); !errors.Is(err, ErrIdentifiantsInvalides) {
		t.Fatalf("mauvais mot de passe : attendu ErrIdentifiantsInvalides, obtenu %v", err)
	}
	if _, err := a.Authentifier("inconnu", "peu importe"); !errors.Is(err, ErrIdentifiantsInvalides) {
		t.Fatalf("login inconnu : attendu ErrIdentifiantsInvalides, obtenu %v", err)
	}
}

func TestAuthentifierCompteInactif(t *testing.T) {
	d := depotDeTest(t)
	u := utilisateurDeTest(t, d, "parti", "s3cret!", depot.RoleLecteur)
	if err := d.ArchiverUtilisateur(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := NouvelAuthenticatorLocal(d).Authentifier("parti", "s3cret!"); !errors.Is(err, ErrCompteInactif) {
		t.Fatalf("compte désactivé : attendu ErrCompteInactif, obtenu %v", err)
	}
}

func TestServiceCycleSession(t *testing.T) {
	d := depotDeTest(t)
	utilisateurDeTest(t, d, "editeur", "s3cret!", depot.RoleEditeur)
	svc := NouveauService(d)

	session, identite, err := svc.Connecter("editeur", "s3cret!")
	if err != nil {
		t.Fatalf("connexion : %v", err)
	}
	if session.Jeton == "" || session.CSRFToken == "" || session.Jeton == session.CSRFToken {
		t.Fatalf("jetons de session invalides : %+v", session)
	}
	if identite.Role != depot.RoleEditeur {
		t.Fatalf("rôle attendu %s, obtenu %s", depot.RoleEditeur, identite.Role)
	}

	relue, identiteRelue, err := svc.Courant(session.Jeton)
	if err != nil {
		t.Fatalf("session courante : %v", err)
	}
	if relue.UtilisateurID != identite.UtilisateurID || identiteRelue.Login != "editeur" {
		t.Fatalf("session/identité relues incohérentes : %+v / %+v", relue, identiteRelue)
	}

	if err := svc.Deconnecter(session.Jeton); err != nil {
		t.Fatalf("déconnexion : %v", err)
	}
	if _, _, err := svc.Courant(session.Jeton); !errors.Is(err, ErrSessionInvalide) {
		t.Fatalf("session déconnectée : attendu ErrSessionInvalide, obtenu %v", err)
	}
}

func TestSessionExpiration(t *testing.T) {
	d := depotDeTest(t)
	u := utilisateurDeTest(t, d, "editeur", "s3cret!", depot.RoleLecteur)
	g := NouveauGestionnaireSessions(d.Base())

	s, err := g.Ouvrir(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// on force l'expiration dans le passé pour ne pas dépendre d'un vrai délai
	if _, err := d.Base().Exec(`UPDATE session SET expire_le = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), s.Jeton); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Charger(s.Jeton); !errors.Is(err, ErrSessionInvalide) {
		t.Fatalf("session expirée : attendu ErrSessionInvalide, obtenu %v", err)
	}
	// Charger doit avoir nettoyé la ligne expirée
	var n int
	d.Base().QueryRow(`SELECT COUNT(*) FROM session WHERE id = ?`, s.Jeton).Scan(&n)
	if n != 0 {
		t.Fatal("la session expirée aurait dû être purgée à la lecture")
	}
}

func TestSessionInvalideeSiCompteDesactive(t *testing.T) {
	d := depotDeTest(t)
	u := utilisateurDeTest(t, d, "editeur", "s3cret!", depot.RoleLecteur)
	svc := NouveauService(d)

	session, _, err := svc.Connecter("editeur", "s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ArchiverUtilisateur(u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Courant(session.Jeton); !errors.Is(err, ErrSessionInvalide) {
		t.Fatalf("compte désactivé après connexion : attendu ErrSessionInvalide, obtenu %v", err)
	}
}
