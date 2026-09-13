package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Zone est un référentiel : un code stable, un libellé et un site
// géographique facultatif (Site nil = non renseigné). Sans drapeau d'activité.
// Sert de portée aux contraintes de répartition d'un cluster.
type Zone struct {
	ID      int64
	Code    string
	Libelle string
	Site    *string
}

// CreerZone insère une zone et renvoie la ligne complète,
// identifiant attribué. Un Site vide ou blanc est normalisé en nil.
//
// Erreurs : ErrValidation si code ou libellé vide ; ErrConflit si le code est
// déjà pris.
func (d *Depot) CreerZone(dc Zone) (Zone, error) {
	dc.Code = strings.TrimSpace(dc.Code)
	dc.Libelle = strings.TrimSpace(dc.Libelle)
	dc.Site = normaliserSite(dc.Site)
	if dc.Code == "" {
		return Zone{}, fmt.Errorf("création d'une zone : %w : code vide", ErrValidation)
	}
	if dc.Libelle == "" {
		return Zone{}, fmt.Errorf("création d'une zone : %w : libellé vide", ErrValidation)
	}

	return creerJournalise(d, "zone", func(tx *sql.Tx) (Zone, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO zone (code, libelle, site) VALUES (?, ?, ?)`,
			dc.Code, dc.Libelle, dc.Site)
		if err != nil {
			return Zone{}, 0, traduire("création de la zone "+dc.Code, err)
		}
		dc.ID, _ = res.LastInsertId()
		return dc, dc.ID, nil
	})
}

// LireZone renvoie une zone par identifiant, ou ErrIntrouvable.
func (d *Depot) LireZone(id int64) (Zone, error) {
	return lireZone(d.base, id)
}

// LireZoneParCode renvoie une zone par son code, ou ErrIntrouvable.
func (d *Depot) LireZoneParCode(code string) (Zone, error) {
	var dc Zone
	err := scanUn(
		d.base.QueryRow(`SELECT id, code, libelle, site FROM zone WHERE code = ?`,
			strings.TrimSpace(code)),
		"lecture de la zone "+code,
		&dc.ID, &dc.Code, &dc.Libelle, &dc.Site)
	return dc, err
}

// ListerZones renvoie tous les zones, triés par code.
func (d *Depot) ListerZones() ([]Zone, error) {
	lignes, err := d.base.Query(
		`SELECT id, code, libelle, site FROM zone ORDER BY code`)
	if err != nil {
		return nil, traduire("liste des zones", err)
	}
	defer lignes.Close()

	var out []Zone
	for lignes.Next() {
		var dc Zone
		if err := lignes.Scan(&dc.ID, &dc.Code, &dc.Libelle, &dc.Site); err != nil {
			return nil, fmt.Errorf("liste des zones : %w", err)
		}
		out = append(out, dc)
	}
	return out, lignes.Err()
}

// ModifierZone met à jour le code, le libellé et le site. Un Site vide
// ou blanc est normalisé en nil.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation si
// code ou libellé vide ; ErrConflit si le nouveau code est déjà pris.
func (d *Depot) ModifierZone(dc Zone) error {
	dc.Code = strings.TrimSpace(dc.Code)
	dc.Libelle = strings.TrimSpace(dc.Libelle)
	dc.Site = normaliserSite(dc.Site)
	if dc.Code == "" || dc.Libelle == "" {
		return fmt.Errorf("modification de la zone %d : %w : code ou libellé vide",
			dc.ID, ErrValidation)
	}

	return modifierJournalise(d, "zone", dc.ID, ActionModification, lireZone, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE zone SET code = ?, libelle = ?, site = ? WHERE id = ?`,
			dc.Code, dc.Libelle, dc.Site, dc.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la zone %d", dc.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification de la zone %d", dc.ID))
	})
}

// ---------------------------------------------------------------- helpers

// normaliserSite ramène un site vide ou uniquement composé d'espaces à nil,
// pour ne pas stocker de chaîne vide là où l'absence de valeur est un NULL.
func normaliserSite(site *string) *string {
	if site == nil {
		return nil
	}
	v := strings.TrimSpace(*site)
	if v == "" {
		return nil
	}
	return &v
}

func lireZone(c conn, id int64) (Zone, error) {
	var dc Zone
	err := scanUn(
		c.QueryRow(`SELECT id, code, libelle, site FROM zone WHERE id = ?`, id),
		fmt.Sprintf("lecture de la zone %d", id),
		&dc.ID, &dc.Code, &dc.Libelle, &dc.Site)
	return dc, err
}
