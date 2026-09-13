package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Rôles reconnus par la colonne utilisateur.role (contrainte CHECK en base).
const (
	RoleAdmin   = "ADMIN"
	RoleEditeur = "EDITEUR"
	RoleLecteur = "LECTEUR"
)

// Utilisateur est un compte applicatif : un login stable, un condensé de mot
// de passe, un nom d'affichage facultatif, un rôle et un drapeau d'activité.
//
// Ce dépôt ne fait aucune logique de mot de passe : il stocke et restitue le
// champ Hash tel quel. Le hachage argon2id et la vérification appartiennent à
// la couche d'authentification.
type Utilisateur struct {
	ID      int64
	Login   string
	Hash    string `json:"-"` // jamais dans le journal ni dans un export
	Nom     *string
	Role    string
	Actif   bool
	CreeLe  string
	Origine string // OrigineLocale | OrigineLDAP (v3.6)
}

// Origine d'un compte : mot de passe vérifié en base, ou par bind sur
// l'annuaire (v3.6). Le rôle est porté par Parallax dans les deux cas.
const (
	OrigineLocale = "LOCAL"
	OrigineLDAP   = "LDAP"
)

// CreerUtilisateur insère un compte et renvoie la ligne complète, identifiant
// attribué et cree_le posé via Horodatage(). Le compte est actif par défaut.
// Un Nom vide ou blanc est normalisé en nil.
//
// Erreurs : ErrValidation si login ou hash vide, ou si le rôle n'est pas l'un
// de RoleAdmin, RoleEditeur, RoleLecteur (la contrainte CHECK de SQLite est
// traduite en ErrValidation) ; ErrConflit si le login est déjà pris.
func (d *Depot) CreerUtilisateur(u Utilisateur) (Utilisateur, error) {
	u.Login = strings.TrimSpace(u.Login)
	u.Hash = strings.TrimSpace(u.Hash)
	u.Role = strings.TrimSpace(u.Role)
	u.Nom = normaliserNom(u.Nom)
	if u.Login == "" {
		return Utilisateur{}, fmt.Errorf("création d'un utilisateur : %w : login vide", ErrValidation)
	}
	if u.Hash == "" {
		return Utilisateur{}, fmt.Errorf("création d'un utilisateur : %w : hash vide", ErrValidation)
	}

	u.CreeLe = Horodatage()
	u.Origine = OrigineLocale
	return d.insererUtilisateur(u)
}

// CreerUtilisateurLDAP crée le compte d'un utilisateur qui vient de réussir
// un bind sur l'annuaire (v3.6) : origine LDAP, aucun hash, rôle LECTEUR —
// un administrateur le promeut ensuite depuis l'écran des comptes.
//
// Erreurs : ErrValidation si le login est vide ; ErrConflit s'il existe déjà.
func (d *Depot) CreerUtilisateurLDAP(login string) (Utilisateur, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return Utilisateur{}, fmt.Errorf("création d'un utilisateur LDAP : %w : login vide", ErrValidation)
	}
	return d.insererUtilisateur(Utilisateur{
		Login: login, Role: RoleLecteur, CreeLe: Horodatage(), Origine: OrigineLDAP,
	})
}

func (d *Depot) insererUtilisateur(u Utilisateur) (Utilisateur, error) {
	return creerJournalise(d, "utilisateur", func(tx *sql.Tx) (Utilisateur, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO utilisateur (login, hash, nom, role, cree_le, origine) VALUES (?, ?, ?, ?, ?, ?)`,
			u.Login, u.Hash, u.Nom, u.Role, u.CreeLe, u.Origine)
		if err != nil {
			return Utilisateur{}, 0, traduire("création de l'utilisateur "+u.Login, err)
		}
		u.ID, _ = res.LastInsertId()
		u.Actif = true
		return u, u.ID, nil
	})
}

// LireUtilisateur renvoie un compte par identifiant, ou ErrIntrouvable.
func (d *Depot) LireUtilisateur(id int64) (Utilisateur, error) {
	return lireUtilisateur(d.base, id)
}

// LireUtilisateurParLogin renvoie un compte par son login, ou ErrIntrouvable.
func (d *Depot) LireUtilisateurParLogin(login string) (Utilisateur, error) {
	var u Utilisateur
	err := scanUn(
		d.base.QueryRow(
			`SELECT id, login, hash, nom, role, actif, cree_le, origine FROM utilisateur WHERE login = ?`,
			strings.TrimSpace(login)),
		"lecture de l'utilisateur "+login,
		&u.ID, &u.Login, &u.Hash, &u.Nom, &u.Role, &u.Actif, &u.CreeLe, &u.Origine)
	return u, err
}

// ListerUtilisateurs renvoie les comptes triés par login. Les comptes
// désactivés sont exclus sauf si inclureInactifs est vrai.
func (d *Depot) ListerUtilisateurs(inclureInactifs bool) ([]Utilisateur, error) {
	requete := `SELECT id, login, hash, nom, role, actif, cree_le, origine FROM utilisateur`
	if !inclureInactifs {
		requete += ` WHERE actif = 1`
	}
	requete += ` ORDER BY login`

	lignes, err := d.base.Query(requete)
	if err != nil {
		return nil, traduire("liste des utilisateurs", err)
	}
	defer lignes.Close()

	var out []Utilisateur
	for lignes.Next() {
		var u Utilisateur
		if err := lignes.Scan(
			&u.ID, &u.Login, &u.Hash, &u.Nom, &u.Role, &u.Actif, &u.CreeLe, &u.Origine); err != nil {
			return nil, fmt.Errorf("liste des utilisateurs : %w", err)
		}
		out = append(out, u)
	}
	return out, lignes.Err()
}

// ModifierUtilisateur met à jour le nom d'affichage et le rôle. Le login se
// fixe à la création ; le condensé se change par DefinirHash ; l'activité se
// pilote par ArchiverUtilisateur / ReactiverUtilisateur.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si le
// rôle n'est pas reconnu (contrainte CHECK traduite).
func (d *Depot) ModifierUtilisateur(u Utilisateur) error {
	u.Role = strings.TrimSpace(u.Role)
	u.Nom = normaliserNom(u.Nom)

	return modifierJournalise(d, "utilisateur", u.ID, ActionModification, lireUtilisateur, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE utilisateur SET nom = ?, role = ? WHERE id = ?`,
			u.Nom, u.Role, u.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de l'utilisateur %d", u.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification de l'utilisateur %d", u.ID))
	})
}

// DefinirHash remplace le condensé de mot de passe d'un compte. La chaîne
// fournie est stockée telle quelle, sans traitement.
//
// Erreurs : ErrValidation si hash vide ; ErrIntrouvable si l'identifiant
// n'existe pas.
func (d *Depot) DefinirHash(id int64, hash string) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return fmt.Errorf("changement de hash de l'utilisateur %d : %w : hash vide",
			id, ErrValidation)
	}

	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "utilisateur", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE utilisateur SET hash = ? WHERE id = ?`, hash, id)
		if err != nil {
			return traduire(fmt.Sprintf("changement de hash de l'utilisateur %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("changement de hash de l'utilisateur %d", id)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "utilisateur", "utilisateur", id, ActionModification, avant)
	})
}

// ArchiverUtilisateur désactive un compte sans le supprimer : il ne peut plus
// se connecter et disparaît des listes courantes, mais les scénarios, règles
// et valeurs de variables qu'il a signés restent tracés. Rien n'est jamais
// détruit.
func (d *Depot) ArchiverUtilisateur(id int64) error {
	return modifierJournalise(d, "utilisateur", id, ActionModification, lireUtilisateur, func(tx *sql.Tx) error {
		return basculerActif(tx, "utilisateur", id, false)
	})
}

// ReactiverUtilisateur réactive un compte archivé.
func (d *Depot) ReactiverUtilisateur(id int64) error {
	return modifierJournalise(d, "utilisateur", id, ActionModification, lireUtilisateur, func(tx *sql.Tx) error {
		return basculerActif(tx, "utilisateur", id, true)
	})
}

// ---------------------------------------------------------------- helpers

// normaliserNom ramène un nom vide ou uniquement composé d'espaces à nil.
func normaliserNom(nom *string) *string {
	if nom == nil {
		return nil
	}
	v := strings.TrimSpace(*nom)
	if v == "" {
		return nil
	}
	return &v
}

func lireUtilisateur(c conn, id int64) (Utilisateur, error) {
	var u Utilisateur
	err := scanUn(
		c.QueryRow(
			`SELECT id, login, hash, nom, role, actif, cree_le, origine FROM utilisateur WHERE id = ?`, id),
		fmt.Sprintf("lecture de l'utilisateur %d", id),
		&u.ID, &u.Login, &u.Hash, &u.Nom, &u.Role, &u.Actif, &u.CreeLe, &u.Origine)
	return u, err
}
