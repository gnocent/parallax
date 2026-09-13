package depot

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Modele est une génération matérielle : un couple Type+Année correspondant à
// une commande annuelle (ex. DENSE-2025). Il porte le financement et les deux
// coûts (prix fournisseur amont, coût annuel d'hébergement). La capacité brute
// n'est pas ici : elle vit dans les composants des révisions.
//
// Les colonnes nullables sont des pointeurs : nil signifie « non renseigné ».
type Modele struct {
	ID                int64
	Type              string
	Annee             int64
	Code              string
	Description       *string
	ModeFinancement   *string // ACHAT | LEASE, contrôlé par la base
	DureeLeaseMois    *int64
	DateDebutLease    *string // ISO YYYY-MM-DD
	PrixFournisseurHT *float64
	CoutAnnuelHT      *float64
	DureeCoutAnnees   *int64
	Actif             bool
}

// colonnesModele fige l'ordre des colonnes partagé par toutes les lectures.
const colonnesModele = `id, type, annee, code, description, mode_financement,
	duree_lease_mois, date_debut_lease, prix_fournisseur_ht, cout_annuel_ht,
	duree_cout_annees, actif`

// pointeursModele renvoie les cibles de Scan dans l'ordre de colonnesModele.
func pointeursModele(m *Modele) []any {
	return []any{&m.ID, &m.Type, &m.Annee, &m.Code, &m.Description,
		&m.ModeFinancement, &m.DureeLeaseMois, &m.DateDebutLease,
		&m.PrixFournisseurHT, &m.CoutAnnuelHT, &m.DureeCoutAnnees, &m.Actif}
}

// valider vérifie les règles indépendantes de la base : type et code non vides,
// année plausible, date de début de lease bien formée. Le contrôle de
// mode_financement est délégué au CHECK SQLite (traduit en ErrValidation).
func (m *Modele) valider(contexte string) error {
	m.Type = strings.TrimSpace(m.Type)
	m.Code = strings.TrimSpace(m.Code)
	if m.Type == "" {
		return fmt.Errorf("%s : %w : type vide", contexte, ErrValidation)
	}
	if m.Code == "" {
		return fmt.Errorf("%s : %w : code vide", contexte, ErrValidation)
	}
	if m.Annee <= 2000 {
		return fmt.Errorf("%s : %w : année « %d » implausible", contexte, ErrValidation, m.Annee)
	}
	return ValiderDateOpt("date_debut_lease", m.DateDebutLease)
}

// CreerModele insère un modèle et renvoie la ligne complète, identifiant
// attribué. Le modèle est actif par défaut.
//
// Erreurs : ErrValidation si type ou code vide, année <= 2000, date de lease
// mal formée ou mode de financement hors ACHAT|LEASE ; ErrConflit si le code
// ou le couple (type, année) est déjà pris.
func (d *Depot) CreerModele(m Modele) (Modele, error) {
	if err := m.valider("création d'un modèle"); err != nil {
		return Modele{}, err
	}

	err := d.enTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO modele (type, annee, code, description, mode_financement,
				duree_lease_mois, date_debut_lease, prix_fournisseur_ht,
				cout_annuel_ht, duree_cout_annees)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Type, m.Annee, m.Code, m.Description, m.ModeFinancement,
			m.DureeLeaseMois, m.DateDebutLease, m.PrixFournisseurHT,
			m.CoutAnnuelHT, m.DureeCoutAnnees)
		if err != nil {
			return traduire("création du modèle "+m.Code, err)
		}
		m.ID, _ = res.LastInsertId()
		m.Actif = true
		return d.journalCreationTable(tx, "modele", "modele", m.ID)
	})
	if err != nil {
		return Modele{}, err
	}
	return m, nil
}

// LireModele renvoie un modèle par identifiant, ou ErrIntrouvable.
func (d *Depot) LireModele(id int64) (Modele, error) {
	var m Modele
	err := scanUn(
		d.base.QueryRow(`SELECT `+colonnesModele+` FROM modele WHERE id = ?`, id),
		fmt.Sprintf("lecture du modèle %d", id),
		pointeursModele(&m)...)
	return m, err
}

// LireModeleParCode renvoie un modèle par son code (clé de jointure
// historique), ou ErrIntrouvable.
func (d *Depot) LireModeleParCode(code string) (Modele, error) {
	code = strings.TrimSpace(code)
	var m Modele
	err := scanUn(
		d.base.QueryRow(`SELECT `+colonnesModele+` FROM modele WHERE code = ?`, code),
		"lecture du modèle "+code,
		pointeursModele(&m)...)
	return m, err
}

// ListerModeles renvoie les modèles triés par code. Les modèles archivés sont
// exclus sauf si inclureInactifs est vrai.
func (d *Depot) ListerModeles(inclureInactifs bool) ([]Modele, error) {
	requete := `SELECT ` + colonnesModele + ` FROM modele`
	if !inclureInactifs {
		requete += ` WHERE actif = 1`
	}
	requete += ` ORDER BY code`

	lignes, err := d.base.Query(requete)
	if err != nil {
		return nil, traduire("liste des modèles", err)
	}
	defer lignes.Close()

	var out []Modele
	for lignes.Next() {
		var m Modele
		if err := lignes.Scan(pointeursModele(&m)...); err != nil {
			return nil, fmt.Errorf("liste des modèles : %w", err)
		}
		out = append(out, m)
	}
	return out, lignes.Err()
}

// ModifierModele met à jour tous les champs descriptifs, financiers et de
// coût. L'activité se pilote par ArchiverModele / ReactiverModele.
//
// Erreurs : ErrIntrouvable si l'identifiant n'existe pas ; ErrValidation comme
// CreerModele ; ErrConflit si le nouveau code ou couple (type, année) est pris.
func (d *Depot) ModifierModele(m Modele) error {
	if err := m.valider(fmt.Sprintf("modification du modèle %d", m.ID)); err != nil {
		return err
	}

	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "modele", m.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE modele SET type = ?, annee = ?, code = ?, description = ?,
				mode_financement = ?, duree_lease_mois = ?, date_debut_lease = ?,
				prix_fournisseur_ht = ?, cout_annuel_ht = ?, duree_cout_annees = ?
			 WHERE id = ?`,
			m.Type, m.Annee, m.Code, m.Description, m.ModeFinancement,
			m.DureeLeaseMois, m.DateDebutLease, m.PrixFournisseurHT,
			m.CoutAnnuelHT, m.DureeCoutAnnees, m.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du modèle %d", m.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification du modèle %d", m.ID)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "modele", "modele", m.ID, ActionModification, avant)
	})
}

// ArchiverModele désactive un modèle sans le supprimer : il disparaît des
// listes courantes mais reste lisible, et ses révisions comme les serveurs qui
// les portent restent intacts. Rien n'est jamais détruit.
func (d *Depot) ArchiverModele(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "modele", id)
		if err != nil {
			return err
		}
		if err := basculerActif(tx, "modele", id, false); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "modele", "modele", id, ActionModification, avant)
	})
}

// ReactiverModele réactive un modèle archivé.
func (d *Depot) ReactiverModele(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "modele", id)
		if err != nil {
			return err
		}
		if err := basculerActif(tx, "modele", id, true); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "modele", "modele", id, ActionModification, avant)
	})
}

// FinLease calcule la date de fin de lease à partir de la date de début et
// d'une durée en mois : date_debut_lease + dureeMois mois, au format ISO
// YYYY-MM-DD. C'est la dérivation de l'ancienne colonne Excel « FinLease ».
//
// L'arithmétique de mois suit la sémantique de time.AddDate : un jour qui
// n'existe pas dans le mois cible déborde sur le mois suivant (31 janvier
// + 1 mois → 3 mars). Les débuts de lease étant en pratique calés sur le
// premier du mois, le cas ne se présente pas.
//
// Erreurs : ErrValidation si la date est mal formée ou la durée <= 0.
func FinLease(dateDebutLease string, dureeMois int64) (string, error) {
	debut, err := time.Parse("2006-01-02", dateDebutLease)
	if err != nil {
		return "", fmt.Errorf("%w : date_debut_lease « %s » n'est pas une date YYYY-MM-DD",
			ErrValidation, dateDebutLease)
	}
	if dureeMois <= 0 {
		return "", fmt.Errorf("%w : durée de lease « %d » mois non positive",
			ErrValidation, dureeMois)
	}
	return debut.AddDate(0, int(dureeMois), 0).Format("2006-01-02"), nil
}
