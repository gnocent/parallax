package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Tier est un référentiel ordonné, calqué sur Environnement : un code stable,
// un libellé et un rang d'affichage, sans drapeau d'activité. Recense les
// niveaux de service d'un cluster (HOT, WARM, COLD…).
type Tier struct {
	ID      int64
	Code    string
	Libelle string
	Ordre   int
}

// CreerTier insère un tier et renvoie la ligne complète, identifiant attribué.
//
// Erreurs : ErrValidation si code ou libellé vide ; ErrConflit si le code est
// déjà pris.
func (d *Depot) CreerTier(t Tier) (Tier, error) {
	t.Code = strings.TrimSpace(t.Code)
	t.Libelle = strings.TrimSpace(t.Libelle)
	if t.Code == "" {
		return Tier{}, fmt.Errorf("création d'un tier : %w : code vide", ErrValidation)
	}
	if t.Libelle == "" {
		return Tier{}, fmt.Errorf("création d'un tier : %w : libellé vide", ErrValidation)
	}

	return creerJournalise(d, "tier", func(tx *sql.Tx) (Tier, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO tier (code, libelle, ordre) VALUES (?, ?, ?)`,
			t.Code, t.Libelle, t.Ordre)
		if err != nil {
			return Tier{}, 0, traduire("création du tier "+t.Code, err)
		}
		t.ID, _ = res.LastInsertId()
		return t, t.ID, nil
	})
}

// LireTier renvoie un tier par identifiant, ou ErrIntrouvable.
func (d *Depot) LireTier(id int64) (Tier, error) {
	return lireTier(d.base, id)
}

// LireTierParCode renvoie un tier par son code, ou ErrIntrouvable.
func (d *Depot) LireTierParCode(code string) (Tier, error) {
	var t Tier
	err := scanUn(
		d.base.QueryRow(`SELECT id, code, libelle, ordre FROM tier WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture du tier "+code,
		&t.ID, &t.Code, &t.Libelle, &t.Ordre)
	return t, err
}

// ListerTiers renvoie tous les tiers, triés par ordre croissant puis par code.
func (d *Depot) ListerTiers() ([]Tier, error) {
	lignes, err := d.base.Query(
		`SELECT id, code, libelle, ordre FROM tier ORDER BY ordre, code`)
	if err != nil {
		return nil, traduire("liste des tiers", err)
	}
	defer lignes.Close()

	var out []Tier
	for lignes.Next() {
		var t Tier
		if err := lignes.Scan(&t.ID, &t.Code, &t.Libelle, &t.Ordre); err != nil {
			return nil, fmt.Errorf("liste des tiers : %w", err)
		}
		out = append(out, t)
	}
	return out, lignes.Err()
}

// ModifierTier met à jour le code, le libellé et le rang d'affichage.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si
// code ou libellé vide ; ErrConflit si le nouveau code est déjà pris.
func (d *Depot) ModifierTier(t Tier) error {
	t.Code = strings.TrimSpace(t.Code)
	t.Libelle = strings.TrimSpace(t.Libelle)
	if t.Code == "" || t.Libelle == "" {
		return fmt.Errorf("modification du tier %d : %w : code ou libellé vide",
			t.ID, ErrValidation)
	}

	return modifierJournalise(d, "tier", t.ID, ActionModification, lireTier, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE tier SET code = ?, libelle = ?, ordre = ? WHERE id = ?`,
			t.Code, t.Libelle, t.Ordre, t.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du tier %d", t.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification du tier %d", t.ID))
	})
}

// ---------------------------------------------------------------- helpers

func lireTier(c conn, id int64) (Tier, error) {
	var t Tier
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, ordre FROM tier WHERE id = ?`, id),
		fmt.Sprintf("lecture du tier %d", id),
		&t.ID, &t.Code, &t.Libelle, &t.Ordre)
	return t, err
}
