package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Metrique est une grandeur dimensionnable : DISQUE_UTILE_TO, CPU_CORES,
// RAM_GO, NOEUDS, LICENCES… Une règle produit un besoin dans exactement une
// métrique.
type Metrique struct {
	ID      int64
	Code    string
	Libelle string
	Unite   string
}

// CreerMetrique insère une métrique. code, libellé et unité sont obligatoires.
func (d *Depot) CreerMetrique(m Metrique) (Metrique, error) {
	m.Code = strings.TrimSpace(m.Code)
	m.Libelle = strings.TrimSpace(m.Libelle)
	m.Unite = strings.TrimSpace(m.Unite)
	if m.Code == "" || m.Libelle == "" || m.Unite == "" {
		return Metrique{}, fmt.Errorf(
			"création d'une métrique : %w : code, libellé et unité obligatoires", ErrValidation)
	}
	return creerJournalise(d, "metrique", func(tx *sql.Tx) (Metrique, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO metrique (code, libelle, unite) VALUES (?, ?, ?)`,
			m.Code, m.Libelle, m.Unite)
		if err != nil {
			return Metrique{}, 0, traduire("création de la métrique "+m.Code, err)
		}
		m.ID, _ = res.LastInsertId()
		return m, m.ID, nil
	})
}

// LireMetrique renvoie une métrique par identifiant, ou ErrIntrouvable.
func (d *Depot) LireMetrique(id int64) (Metrique, error) {
	return lireMetrique(d.base, id)
}

func lireMetrique(c conn, id int64) (Metrique, error) {
	var m Metrique
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, unite FROM metrique WHERE id = ?`, id),
		fmt.Sprintf("lecture de la métrique %d", id),
		&m.ID, &m.Code, &m.Libelle, &m.Unite)
	return m, err
}

// LireMetriqueParCode renvoie une métrique par son code, ou ErrIntrouvable.
func (d *Depot) LireMetriqueParCode(code string) (Metrique, error) {
	var m Metrique
	err := scanUn(
		d.base.QueryRow(`SELECT id, code, libelle, unite FROM metrique WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture de la métrique "+code,
		&m.ID, &m.Code, &m.Libelle, &m.Unite)
	return m, err
}

// ListerMetriques renvoie toutes les métriques, triées par code.
func (d *Depot) ListerMetriques() ([]Metrique, error) {
	lignes, err := d.base.Query(`SELECT id, code, libelle, unite FROM metrique ORDER BY code`)
	if err != nil {
		return nil, traduire("liste des métriques", err)
	}
	defer lignes.Close()

	var out []Metrique
	for lignes.Next() {
		var m Metrique
		if err := lignes.Scan(&m.ID, &m.Code, &m.Libelle, &m.Unite); err != nil {
			return nil, fmt.Errorf("liste des métriques : %w", err)
		}
		out = append(out, m)
	}
	return out, lignes.Err()
}

// ModifierMetrique met à jour le libellé et l'unité (le code reste stable).
func (d *Depot) ModifierMetrique(m Metrique) error {
	m.Libelle = strings.TrimSpace(m.Libelle)
	m.Unite = strings.TrimSpace(m.Unite)
	if m.Libelle == "" || m.Unite == "" {
		return fmt.Errorf("modification de la métrique %d : %w : libellé ou unité vide",
			m.ID, ErrValidation)
	}
	return modifierJournalise(d, "metrique", m.ID, ActionModification, lireMetrique, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE metrique SET libelle = ?, unite = ? WHERE id = ?`,
			m.Libelle, m.Unite, m.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la métrique %d", m.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification de la métrique %d", m.ID))
	})
}
