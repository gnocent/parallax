package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Environnement est un référentiel ordonné : un code stable, un libellé et un
// rang d'affichage. Contrairement à Projet ou Techno, il ne porte pas de
// drapeau d'activité — les environnements (PROD, QUAL…) sont un socle fixe.
type Environnement struct {
	ID      int64
	Code    string
	Libelle string
	Ordre   int
}

// CreerEnvironnement insère un environnement et renvoie la ligne complète,
// identifiant attribué.
//
// Erreurs : ErrValidation si code ou libellé vide ; ErrConflit si le code est
// déjà pris.
func (d *Depot) CreerEnvironnement(e Environnement) (Environnement, error) {
	e.Code = strings.TrimSpace(e.Code)
	e.Libelle = strings.TrimSpace(e.Libelle)
	if e.Code == "" {
		return Environnement{}, fmt.Errorf("création d'un environnement : %w : code vide", ErrValidation)
	}
	if e.Libelle == "" {
		return Environnement{}, fmt.Errorf("création d'un environnement : %w : libellé vide", ErrValidation)
	}

	return creerJournalise(d, "environnement", func(tx *sql.Tx) (Environnement, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO environnement (code, libelle, ordre) VALUES (?, ?, ?)`,
			e.Code, e.Libelle, e.Ordre)
		if err != nil {
			return Environnement{}, 0, traduire("création de l'environnement "+e.Code, err)
		}
		e.ID, _ = res.LastInsertId()
		return e, e.ID, nil
	})
}

// LireEnvironnement renvoie un environnement par identifiant, ou ErrIntrouvable.
func (d *Depot) LireEnvironnement(id int64) (Environnement, error) {
	return lireEnvironnement(d.base, id)
}

// LireEnvironnementParCode renvoie un environnement par son code, ou
// ErrIntrouvable.
func (d *Depot) LireEnvironnementParCode(code string) (Environnement, error) {
	var e Environnement
	err := scanUn(
		d.base.QueryRow(
			`SELECT id, code, libelle, ordre FROM environnement WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture de l'environnement "+code,
		&e.ID, &e.Code, &e.Libelle, &e.Ordre)
	return e, err
}

// ListerEnvironnements renvoie tous les environnements, triés par ordre
// croissant puis par code.
func (d *Depot) ListerEnvironnements() ([]Environnement, error) {
	lignes, err := d.base.Query(
		`SELECT id, code, libelle, ordre FROM environnement ORDER BY ordre, code`)
	if err != nil {
		return nil, traduire("liste des environnements", err)
	}
	defer lignes.Close()

	var out []Environnement
	for lignes.Next() {
		var e Environnement
		if err := lignes.Scan(&e.ID, &e.Code, &e.Libelle, &e.Ordre); err != nil {
			return nil, fmt.Errorf("liste des environnements : %w", err)
		}
		out = append(out, e)
	}
	return out, lignes.Err()
}

// ModifierEnvironnement met à jour le code, le libellé et le rang d'affichage.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si
// code ou libellé vide ; ErrConflit si le nouveau code est déjà pris.
func (d *Depot) ModifierEnvironnement(e Environnement) error {
	e.Code = strings.TrimSpace(e.Code)
	e.Libelle = strings.TrimSpace(e.Libelle)
	if e.Code == "" || e.Libelle == "" {
		return fmt.Errorf("modification de l'environnement %d : %w : code ou libellé vide",
			e.ID, ErrValidation)
	}

	return modifierJournalise(d, "environnement", e.ID, ActionModification, lireEnvironnement, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE environnement SET code = ?, libelle = ?, ordre = ? WHERE id = ?`,
			e.Code, e.Libelle, e.Ordre, e.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de l'environnement %d", e.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification de l'environnement %d", e.ID))
	})
}

// ---------------------------------------------------------------- helpers

func lireEnvironnement(c conn, id int64) (Environnement, error) {
	var e Environnement
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, ordre FROM environnement WHERE id = ?`, id),
		fmt.Sprintf("lecture de l'environnement %d", id),
		&e.ID, &e.Code, &e.Libelle, &e.Ordre)
	return e, err
}
