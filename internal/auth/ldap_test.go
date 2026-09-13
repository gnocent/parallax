package auth

import (
	"errors"
	"testing"

	"parallax/internal/depot"
	"parallax/internal/ldap"
)

// lieurFactice joue l'annuaire : un seul couple accepté, ou une panne.
type lieurFactice struct {
	login, mdp string
	panne      error
	appels     int
}

func (l *lieurFactice) Lier(login, mdp string) error {
	l.appels++
	if l.panne != nil {
		return l.panne
	}
	if login == l.login && mdp == l.mdp {
		return nil
	}
	return ldap.ErrRefus
}

// TestAuthenticatorLDAPCombine : compte local → argon2id sans toucher
// l'annuaire ; inconnu → bind puis création en LECTEUR ; compte LDAP
// existant → bind ; désactivé → refusé avant tout bind ; annuaire en panne
// → erreur technique, pas « identifiants invalides ».
func TestAuthenticatorLDAPCombine(t *testing.T) {
	d := depotDeTest(t)
	hash, err := HacherMotDePasse("local!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreerUtilisateur(depot.Utilisateur{Login: "admin", Hash: hash, Role: depot.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	annuaire := &lieurFactice{login: "alice", mdp: "s3cret!"}
	a := NouvelAuthenticatorLDAP(d, annuaire)

	// local : l'annuaire n'est jamais sollicité
	if id, err := a.Authentifier("admin", "local!"); err != nil || id.Role != depot.RoleAdmin {
		t.Fatalf("compte local attendu accepté : %+v, %v", id, err)
	}
	if _, err := a.Authentifier("admin", "faux"); !errors.Is(err, ErrIdentifiantsInvalides) {
		t.Fatalf("compte local, mauvais mot de passe : %v", err)
	}
	if annuaire.appels != 0 {
		t.Fatal("un compte local ne doit pas provoquer de bind")
	}

	// inconnu de Parallax, accepté par l'annuaire : créé en lecteur
	id, err := a.Authentifier("alice", "s3cret!")
	if err != nil || id.Role != depot.RoleLecteur || id.Login != "alice" {
		t.Fatalf("première connexion LDAP : %+v, %v", id, err)
	}
	u, err := d.LireUtilisateurParLogin("alice")
	if err != nil || u.Origine != depot.OrigineLDAP || u.Hash != "" {
		t.Fatalf("compte LDAP attendu sans hash : %+v, %v", u, err)
	}
	journal, _ := d.ListerJournal(depot.FiltreJournal{Entite: "utilisateur", EntiteID: &u.ID})
	if len(journal) != 1 || journal[0].Action != depot.ActionCreation {
		t.Fatalf("la création automatique doit être journalisée : %+v", journal)
	}

	// deuxième connexion : bind, pas de nouvelle création ; promotion locale conservée
	if err := d.ModifierUtilisateur(depot.Utilisateur{ID: u.ID, Login: u.Login, Role: depot.RoleEditeur}); err != nil {
		t.Fatal(err)
	}
	if id, err := a.Authentifier("alice", "s3cret!"); err != nil || id.Role != depot.RoleEditeur || id.UtilisateurID != u.ID {
		t.Fatalf("reconnexion LDAP avec rôle promu : %+v, %v", id, err)
	}
	if _, err := a.Authentifier("alice", "faux"); !errors.Is(err, ErrIdentifiantsInvalides) {
		t.Fatalf("compte LDAP, mauvais mot de passe : %v", err)
	}
	// inconnu partout : refus, rien de créé
	if _, err := a.Authentifier("bob", "x"); !errors.Is(err, ErrIdentifiantsInvalides) {
		t.Fatalf("inconnu de l'annuaire : %v", err)
	}
	if _, err := d.LireUtilisateurParLogin("bob"); !errors.Is(err, depot.ErrIntrouvable) {
		t.Fatal("un refus de l'annuaire ne doit créer aucun compte")
	}

	// désactivé : refusé avant tout bind
	if err := d.ArchiverUtilisateur(u.ID); err != nil {
		t.Fatal(err)
	}
	appels := annuaire.appels
	if _, err := a.Authentifier("alice", "s3cret!"); !errors.Is(err, ErrCompteInactif) || annuaire.appels != appels {
		t.Fatalf("compte désactivé : attendu ErrCompteInactif sans bind, obtenu %v", err)
	}

	// annuaire en panne : erreur technique, le compte local passe toujours
	annuaire.panne = errors.New("connexion refusée")
	if _, err := a.Authentifier("carol", "x"); err == nil || errors.Is(err, ErrIdentifiantsInvalides) {
		t.Fatalf("panne de l'annuaire : attendu une erreur technique, obtenu %v", err)
	}
	if _, err := a.Authentifier("admin", "local!"); err != nil {
		t.Fatalf("l'administrateur local doit passer malgré la panne : %v", err)
	}
}
