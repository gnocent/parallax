package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// UsageFonctionnel est un référentiel minimal : un code stable et un libellé.
// Ni drapeau d'activité, ni rang d'affichage — c'est une simple nomenclature
// des usages métier d'un cluster (log management, APM, métrologie…).
type UsageFonctionnel struct {
	ID      int64
	Code    string
	Libelle string
}

// CreerUsage insère un usage fonctionnel et renvoie la ligne complète,
// identifiant attribué.
//
// Erreurs : ErrValidation si code ou libellé vide ; ErrConflit si le code est
// déjà pris.
func (d *Depot) CreerUsage(u UsageFonctionnel) (UsageFonctionnel, error) {
	u.Code = strings.TrimSpace(u.Code)
	u.Libelle = strings.TrimSpace(u.Libelle)
	if u.Code == "" {
		return UsageFonctionnel{}, fmt.Errorf("création d'un usage : %w : code vide", ErrValidation)
	}
	if u.Libelle == "" {
		return UsageFonctionnel{}, fmt.Errorf("création d'un usage : %w : libellé vide", ErrValidation)
	}

	return creerJournalise(d, "usage", func(tx *sql.Tx) (UsageFonctionnel, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO usage_fonctionnel (code, libelle) VALUES (?, ?)`, u.Code, u.Libelle)
		if err != nil {
			return UsageFonctionnel{}, 0, traduire("création de l'usage "+u.Code, err)
		}
		u.ID, _ = res.LastInsertId()
		return u, u.ID, nil
	})
}

// LireUsage renvoie un usage fonctionnel par identifiant, ou ErrIntrouvable.
func (d *Depot) LireUsage(id int64) (UsageFonctionnel, error) {
	return lireUsage(d.base, id)
}

// LireUsageParCode renvoie un usage fonctionnel par son code, ou
// ErrIntrouvable.
func (d *Depot) LireUsageParCode(code string) (UsageFonctionnel, error) {
	var u UsageFonctionnel
	err := scanUn(
		d.base.QueryRow(`SELECT id, code, libelle FROM usage_fonctionnel WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture de l'usage "+code,
		&u.ID, &u.Code, &u.Libelle)
	return u, err
}

// ListerUsages renvoie tous les usages fonctionnels, triés par code.
func (d *Depot) ListerUsages() ([]UsageFonctionnel, error) {
	lignes, err := d.base.Query(
		`SELECT id, code, libelle FROM usage_fonctionnel ORDER BY code`)
	if err != nil {
		return nil, traduire("liste des usages", err)
	}
	defer lignes.Close()

	var out []UsageFonctionnel
	for lignes.Next() {
		var u UsageFonctionnel
		if err := lignes.Scan(&u.ID, &u.Code, &u.Libelle); err != nil {
			return nil, fmt.Errorf("liste des usages : %w", err)
		}
		out = append(out, u)
	}
	return out, lignes.Err()
}

// ModifierUsage met à jour le code et le libellé.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si
// code ou libellé vide ; ErrConflit si le nouveau code est déjà pris.
func (d *Depot) ModifierUsage(u UsageFonctionnel) error {
	u.Code = strings.TrimSpace(u.Code)
	u.Libelle = strings.TrimSpace(u.Libelle)
	if u.Code == "" || u.Libelle == "" {
		return fmt.Errorf("modification de l'usage %d : %w : code ou libellé vide",
			u.ID, ErrValidation)
	}

	return modifierJournalise(d, "usage", u.ID, ActionModification, lireUsage, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE usage_fonctionnel SET code = ?, libelle = ? WHERE id = ?`,
			u.Code, u.Libelle, u.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de l'usage %d", u.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification de l'usage %d", u.ID))
	})
}

// ---------------------------------------------------------------- helpers

func lireUsage(c conn, id int64) (UsageFonctionnel, error) {
	var u UsageFonctionnel
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle FROM usage_fonctionnel WHERE id = ?`, id),
		fmt.Sprintf("lecture de l'usage %d", id),
		&u.ID, &u.Code, &u.Libelle)
	return u, err
}
