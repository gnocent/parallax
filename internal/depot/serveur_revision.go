package depot

import (
	"database/sql"
	"fmt"
	"time"
)

// Rattachement est une ligne de serveur_revision : la période pendant laquelle
// un serveur porte une révision de modèle donnée. L'immuabilité des révisions
// (invariant 1) fait que ces périodes suffisent à retrouver les caractéristiques
// matérielles d'un serveur à n'importe quelle date passée, sans bitemporalité.
//
// date_fin NULL = rattachement en cours. Invariant 4 : les périodes d'un même
// serveur ne se chevauchent pas — l'unicité du rattachement ouvert est posée en
// base (index partiel de 0002), le non-recouvrement des périodes fermées est
// vérifié ici.
type Rattachement struct {
	ID          int64
	ServeurID   int64
	RevisionID  int64
	DateDebut   string
	DateFin     *string
	Commentaire *string
}

const colonnesRattachement = `id, serveur_id, revision_id, date_debut, date_fin, commentaire`

// RattacherRevision ouvre un nouveau rattachement de révision pour un serveur à
// dateDebut. S'il existe déjà un rattachement ouvert, il est clôturé la veille
// de dateDebut (J-1) pour éviter tout double comptage au jour du basculement
// (bornes inclusives, cohérentes avec SQL_OFFRE de la spécification).
//
// Erreurs : ErrValidation si dateDebut est mal formée, ou si elle n'est pas
// postérieure au début de la période ouverte courante ; ErrChevauchement si la
// nouvelle période recouvre une période déjà fermée ; ErrReference si le serveur
// ou la révision n'existe pas.
func (d *Depot) RattacherRevision(serveurID, revisionID int64, dateDebut string, commentaire *string) error {
	if err := ValiderDate("date de début", dateDebut); err != nil {
		return err
	}

	return d.enTx(func(tx *sql.Tx) error {
		return d.rattacherRevisionTx(tx, serveurID, revisionID, dateDebut, commentaire)
	})
}

// rattacherRevisionTx est le corps de RattacherRevision, réutilisable dans
// une transaction plus large (mise à jour en masse, v3.5). dateDebut est
// supposée validée.
func (d *Depot) rattacherRevisionTx(tx *sql.Tx, serveurID, revisionID int64, dateDebut string, commentaire *string) error {
	{
		ctx := fmt.Sprintf("rattachement du serveur %d à la révision %d", serveurID, revisionID)

		var ouvertID int64
		var ouvertDebut string
		err := tx.QueryRow(
			`SELECT id, date_debut FROM serveur_revision
			 WHERE serveur_id = ? AND date_fin IS NULL`, serveurID).
			Scan(&ouvertID, &ouvertDebut)
		switch {
		case err == nil:
			if dateDebut <= ouvertDebut {
				return fmt.Errorf(
					"%s : %w : la nouvelle date de début %s n'est pas postérieure à la période ouverte (%s)",
					ctx, ErrValidation, dateDebut, ouvertDebut)
			}
			veille, err := dateMoinsUnJour(dateDebut)
			if err != nil {
				return err
			}
			avant, err := instantane(tx, "serveur_revision", ouvertID)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`UPDATE serveur_revision SET date_fin = ? WHERE id = ?`, veille, ouvertID); err != nil {
				return traduire(ctx, err)
			}
			if err := d.journalModificationTable(tx, "serveur_revision", "serveur_revision", ouvertID, ActionModification, avant); err != nil {
				return err
			}
		case err == sql.ErrNoRows:
			// pas de période ouverte : rien à clôturer
		default:
			return fmt.Errorf("%s : %w", ctx, err)
		}

		// Invariant 4 : la nouvelle période [dateDebut, +∞) ne doit recouvrir
		// aucune période restante (toute ligne encore ouverte ou finissant à ou
		// après dateDebut).
		var n int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM serveur_revision
			 WHERE serveur_id = ? AND (date_fin IS NULL OR date_fin >= ?)`,
			serveurID, dateDebut).Scan(&n); err != nil {
			return fmt.Errorf("%s : %w", ctx, err)
		}
		if n > 0 {
			return fmt.Errorf("%s : %w avec une période existante", ctx, ErrChevauchement)
		}

		res, err := tx.Exec(
			`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut, commentaire)
			 VALUES (?, ?, ?, ?)`,
			serveurID, revisionID, dateDebut, commentaire)
		if err != nil {
			return traduire(ctx, err)
		}
		id, _ := res.LastInsertId()
		return d.journalCreationTable(tx, "serveur_revision", "serveur_revision", id)
	}
}

// CloturerRattachement pose la date de fin d'un rattachement.
//
// Erreurs : ErrValidation si dateFin est mal formée ou antérieure au début ;
// ErrChevauchement si la période ainsi fermée recouvre une autre période du
// serveur ; ErrIntrouvable si l'identifiant n'existe pas.
func (d *Depot) CloturerRattachement(id int64, dateFin string) error {
	if err := ValiderDate("date de fin", dateFin); err != nil {
		return err
	}

	return d.enTx(func(tx *sql.Tx) error {
		ctx := fmt.Sprintf("clôture du rattachement %d", id)

		var serveurID int64
		var debut string
		if err := scanUn(
			tx.QueryRow(`SELECT serveur_id, date_debut FROM serveur_revision WHERE id = ?`, id),
			ctx, &serveurID, &debut); err != nil {
			return err
		}
		if dateFin < debut {
			return fmt.Errorf("%s : %w : fin %s antérieure au début %s",
				ctx, ErrValidation, dateFin, debut)
		}

		var n int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM serveur_revision
			 WHERE serveur_id = ? AND id <> ?
			   AND date_debut <= ? AND (date_fin IS NULL OR date_fin >= ?)`,
			serveurID, id, dateFin, debut).Scan(&n); err != nil {
			return fmt.Errorf("%s : %w", ctx, err)
		}
		if n > 0 {
			return fmt.Errorf("%s : %w avec une autre période du serveur", ctx, ErrChevauchement)
		}

		avant, err := instantane(tx, "serveur_revision", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE serveur_revision SET date_fin = ? WHERE id = ?`, dateFin, id)
		if err != nil {
			return traduire(ctx, err)
		}
		if err := exigerUneLigne(res, ctx); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "serveur_revision", "serveur_revision", id, ActionModification, avant)
	})
}

// ListerRattachements renvoie l'historique des rattachements d'un serveur, trié
// par date de début.
func (d *Depot) ListerRattachements(serveurID int64) ([]Rattachement, error) {
	lignes, err := d.base.Query(
		`SELECT `+colonnesRattachement+` FROM serveur_revision
		 WHERE serveur_id = ? ORDER BY date_debut, id`, serveurID)
	if err != nil {
		return nil, traduire(fmt.Sprintf("rattachements du serveur %d", serveurID), err)
	}
	defer lignes.Close()

	var out []Rattachement
	for lignes.Next() {
		var r Rattachement
		if err := lignes.Scan(&r.ID, &r.ServeurID, &r.RevisionID,
			&r.DateDebut, &r.DateFin, &r.Commentaire); err != nil {
			return nil, fmt.Errorf("rattachements du serveur %d : %w", serveurID, err)
		}
		out = append(out, r)
	}
	return out, lignes.Err()
}

// RevisionADate renvoie l'identifiant de la révision effective d'un serveur à
// une date donnée, bornes inclusives des deux côtés (comme SQL_OFFRE).
//
// Erreurs : ErrValidation si la date est mal formée ; ErrIntrouvable si le
// serveur ne porte aucune révision à cette date.
func (d *Depot) RevisionADate(serveurID int64, date string) (int64, error) {
	if err := ValiderDate("date", date); err != nil {
		return 0, err
	}
	var revisionID int64
	err := scanUn(
		d.base.QueryRow(
			`SELECT revision_id FROM serveur_revision
			 WHERE serveur_id = ? AND date_debut <= ?
			   AND (date_fin IS NULL OR date_fin >= ?)
			 ORDER BY date_debut DESC LIMIT 1`,
			serveurID, date, date),
		fmt.Sprintf("révision du serveur %d au %s", serveurID, date),
		&revisionID)
	return revisionID, err
}

// ---------------------------------------------------------------- helpers dates

// dateMoinsUnJour renvoie la veille d'une date ISO-8601. Sert à clôturer une
// période à J-1 quand une nouvelle période s'ouvre au jour J, pour que les deux
// ne soient pas actives simultanément au sens des bornes inclusives.
func dateMoinsUnJour(d string) (string, error) {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return "", fmt.Errorf("%w : « %s » n'est pas une date YYYY-MM-DD", ErrValidation, d)
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02"), nil
}

// ajouterMois décale une date ISO-8601 d'un nombre de mois. Sert au calcul de la
// fin de lease (date_debut_lease + duree_lease_mois).
func ajouterMois(d string, mois int) (string, error) {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return "", fmt.Errorf("%w : « %s » n'est pas une date YYYY-MM-DD", ErrValidation, d)
	}
	return t.AddDate(0, mois, 0).Format("2006-01-02"), nil
}
