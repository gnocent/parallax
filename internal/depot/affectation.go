package depot

import (
	"database/sql"
	"fmt"
	"sort"
)

// Affectation est un lien daté serveur → cluster. Elle porte les vies
// successives d'un serveur : réaffectation à un autre projet en fin de lease,
// déménagement, réinstallation. date_fin NULL = affectation en cours.
//
// scenario_id NULL = le réel. Le réel et chaque scénario forment des « seaux »
// distincts : un serveur peut avoir une affectation active dans le réel et une
// autre, en parallèle, dans un scénario. Le seau d'écriture est défini par
// COALESCE(scenario_id, -1), exactement comme l'index idx_affectation_active_unique
// de la migration 0002.
//
// Invariant 3 : au plus une affectation active par (serveur, seau). L'unicité
// est posée en base ; ce dépôt la vérifie d'abord pour rendre un message clair,
// et contrôle en plus le non-chevauchement des périodes fermées du même seau.
type Affectation struct {
	ID          int64
	ServeurID   int64
	ClusterID   int64
	DateDebut   string
	DateFin     *string
	ScenarioID  *int64
	Commentaire *string
}

// VieServeur est une affectation enrichie du cluster visé, pour l'affichage
// chronologique des vies d'un serveur.
type VieServeur struct {
	Affectation
	ClusterNom      string
	ClusterProjetID int64
}

const colonnesAffectation = `id, serveur_id, cluster_id, date_debut, date_fin, scenario_id, commentaire`

func scanAffectation(s interface{ Scan(...any) error }) (Affectation, error) {
	var a Affectation
	err := s.Scan(&a.ID, &a.ServeurID, &a.ClusterID, &a.DateDebut,
		&a.DateFin, &a.ScenarioID, &a.Commentaire)
	return a, err
}

// Affecter ouvre une affectation d'un serveur vers un cluster à dateDebut, dans
// le seau du scénario fourni (nil = réel).
//
// Erreurs : ErrValidation si dateDebut est mal formée ; ErrChevauchement si le
// serveur a déjà une affectation active dans ce seau, ou si la nouvelle période
// recouvre une période existante du même seau ; ErrReference si le serveur, le
// cluster ou le scénario n'existe pas.
func (d *Depot) Affecter(serveurID, clusterID int64, dateDebut string, scenarioID *int64, commentaire *string) error {
	if err := ValiderDate("date de début", dateDebut); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		return d.affecterTx(tx, serveurID, clusterID, dateDebut, scenarioID, commentaire)
	})
}

// poserDateFin pose la date de fin d'une ligne d'affectation et journalise
// la modification — le geste commun de Desaffecter, Reaffecter,
// AffecterDansScenario et retirerDuScenarioTx.
func (d *Depot) poserDateFin(tx *sql.Tx, ctx string, affectationID int64, dateFin string) error {
	avant, err := instantane(tx, "affectation", affectationID)
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE affectation SET date_fin = ? WHERE id = ?`, dateFin, affectationID)
	if err != nil {
		return traduire(ctx, err)
	}
	if err := exigerUneLigne(res, ctx); err != nil {
		return err
	}
	return d.journalModificationTable(tx, "affectation", "affectation", affectationID, ActionModification, avant)
}

// affecterTx contient la logique d'ouverture d'affectation, réutilisable dans
// une transaction (Affecter, Reaffecter, Materialiser). dateDebut est
// supposée déjà validée. La création est journalisée.
func (d *Depot) affecterTx(c conn, serveurID, clusterID int64, dateDebut string, scenarioID *int64, commentaire *string) error {
	ctx := fmt.Sprintf("affectation du serveur %d au cluster %d", serveurID, clusterID)

	var actives int
	if err := c.QueryRow(
		`SELECT COUNT(*) FROM affectation
		 WHERE serveur_id = ? AND date_fin IS NULL
		   AND COALESCE(scenario_id, -1) = COALESCE(?, -1)`,
		serveurID, scenarioID).Scan(&actives); err != nil {
		return fmt.Errorf("%s : %w", ctx, err)
	}
	if actives > 0 {
		return fmt.Errorf(
			"%s : %w : le serveur a déjà une affectation active dans ce seau (invariant 3)",
			ctx, ErrChevauchement)
	}

	// La nouvelle période [dateDebut, +∞) recouvre toute ligne du même seau
	// encore ouverte ou finissant à ou après dateDebut.
	var recouvre int
	if err := c.QueryRow(
		`SELECT COUNT(*) FROM affectation
		 WHERE serveur_id = ? AND COALESCE(scenario_id, -1) = COALESCE(?, -1)
		   AND (date_fin IS NULL OR date_fin >= ?)`,
		serveurID, scenarioID, dateDebut).Scan(&recouvre); err != nil {
		return fmt.Errorf("%s : %w", ctx, err)
	}
	if recouvre > 0 {
		return fmt.Errorf("%s : %w avec une période existante du même seau", ctx, ErrChevauchement)
	}

	res, err := c.Exec(
		`INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id, commentaire)
		 VALUES (?, ?, ?, ?, ?)`,
		serveurID, clusterID, dateDebut, scenarioID, commentaire)
	if err != nil {
		return traduire(ctx, err)
	}
	id, _ := res.LastInsertId()
	return d.journalCreationTable(c, "affectation", "affectation", id)
}

// Desaffecter clôt une affectation ouverte à dateFin.
//
// Erreurs : ErrValidation si dateFin est mal formée, antérieure au début, ou si
// l'affectation est déjà close ; ErrIntrouvable si l'identifiant n'existe pas.
func (d *Depot) Desaffecter(affectationID int64, dateFin string) error {
	if err := ValiderDate("date de fin", dateFin); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		ctx := fmt.Sprintf("désaffectation %d", affectationID)

		var debut string
		var fin *string
		if err := scanUn(
			tx.QueryRow(`SELECT date_debut, date_fin FROM affectation WHERE id = ?`, affectationID),
			ctx, &debut, &fin); err != nil {
			return err
		}
		if fin != nil {
			return fmt.Errorf("%s : %w : affectation déjà close le %s", ctx, ErrValidation, *fin)
		}
		if dateFin < debut {
			return fmt.Errorf("%s : %w : fin %s antérieure au début %s",
				ctx, ErrValidation, dateFin, debut)
		}

		return d.poserDateFin(tx, ctx, affectationID, dateFin)
	})
}

// Reaffecter est le geste « migration = réaffectation » du cadrage : dans une
// seule transaction, l'affectation ouverte courante du serveur (dans le seau
// visé) est clôturée à la veille de date, et une nouvelle affectation est
// ouverte vers nouveauClusterID à date.
//
// Erreurs : ErrIntrouvable si le serveur n'a pas d'affectation active dans ce
// seau ; ErrValidation si date est mal formée ou n'est pas postérieure au début
// de l'affectation courante ; ErrChevauchement, ErrReference comme Affecter.
func (d *Depot) Reaffecter(serveurID, nouveauClusterID int64, date string, scenarioID *int64, commentaire *string) error {
	if err := ValiderDate("date", date); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		return d.reaffecterTx(tx, serveurID, nouveauClusterID, date, scenarioID, commentaire)
	})
}

// reaffecterTx est le corps de Reaffecter, réutilisable dans une transaction
// plus large (mise à jour en masse, v3.5). date est supposée validée.
func (d *Depot) reaffecterTx(tx *sql.Tx, serveurID, nouveauClusterID int64, date string, scenarioID *int64, commentaire *string) error {
	{
		ctx := fmt.Sprintf("réaffectation du serveur %d", serveurID)

		var couranteID int64
		var couranteDebut string
		if err := scanUn(
			tx.QueryRow(
				`SELECT id, date_debut FROM affectation
				 WHERE serveur_id = ? AND date_fin IS NULL
				   AND COALESCE(scenario_id, -1) = COALESCE(?, -1)`,
				serveurID, scenarioID),
			ctx+" : aucune affectation active à clôturer", &couranteID, &couranteDebut); err != nil {
			return err
		}

		veille, err := dateMoinsUnJour(date)
		if err != nil {
			return err
		}
		if veille < couranteDebut {
			return fmt.Errorf(
				"%s : %w : la date %s n'est pas postérieure au début de l'affectation courante (%s)",
				ctx, ErrValidation, date, couranteDebut)
		}
		if err := d.poserDateFin(tx, ctx, couranteID, veille); err != nil {
			return err
		}

		return d.affecterTx(tx, serveurID, nouveauClusterID, date, scenarioID, commentaire)
	}
}

// AffecterDansScenario ouvre ou déplace l'affectation d'un serveur dans le
// seau d'un scénario, sans jamais toucher le réel : si le scénario porte déjà
// une affectation active pour ce serveur, elle est close et une nouvelle
// ouverte (même geste que Reaffecter, mais entièrement à l'intérieur du seau
// du scénario) ; sinon, une simple ouverture — le seau est vierge pour ce
// serveur, rien à clôturer. C'est le geste « déplacer » du §6 de
// modele-donnees.md, aussi bien pour un serveur qui a une vie réelle que pour
// un serveur hypothétique déjà présent ailleurs dans ce même scénario.
//
// Erreurs : ErrValidation si date est mal formée ou n'est pas postérieure au
// début de l'affectation du scénario à clôturer (le cas échéant) ;
// ErrChevauchement, ErrReference comme Affecter.
func (d *Depot) AffecterDansScenario(serveurID, clusterID int64, date string, scenarioID int64, commentaire *string) error {
	if err := ValiderDate("date", date); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		ctx := fmt.Sprintf("déplacement du serveur %d dans le scénario %d", serveurID, scenarioID)

		var activeID int64
		var activeDebut string
		err := tx.QueryRow(
			`SELECT id, date_debut FROM affectation
			 WHERE serveur_id = ? AND scenario_id = ? AND date_fin IS NULL`,
			serveurID, scenarioID).Scan(&activeID, &activeDebut)
		switch {
		case err == nil:
			veille, err := dateMoinsUnJour(date)
			if err != nil {
				return err
			}
			if veille < activeDebut {
				return fmt.Errorf(
					"%s : %w : la date %s n'est pas postérieure au début de l'affectation courante du scénario (%s)",
					ctx, ErrValidation, date, activeDebut)
			}
			if err := d.poserDateFin(tx, ctx, activeID, veille); err != nil {
				return err
			}
		case err == sql.ErrNoRows:
			// rien à clôturer : première ligne du scénario pour ce serveur.
		default:
			return fmt.Errorf("%s : %w", ctx, err)
		}

		return d.affecterTx(tx, serveurID, clusterID, date, &scenarioID, commentaire)
	})
}

// RetirerDuScenario retire un serveur d'un cluster, dans un scénario, sans le
// réaffecter ailleurs — le geste « retirer » du §6 de modele-donnees.md :
// ouvre si besoin une ligne dans le seau du scénario, calquée sur
// l'affectation réelle active du serveur, puis pose sa date de fin à dateFin.
// Un second retrait ne crée pas de doublon : c'est la même ligne du scénario
// (la plus récente) qui voit sa date de fin déplacée, qu'elle soit encore
// ouverte ou déjà refermée par un retrait précédent.
//
// C'est ce mécanisme qui fait gagner la surcharge dans AffectationsResolues,
// sans qu'il faille de cluster_id nullable ni de représentation séparée d'un
// « retrait ».
//
// Erreurs : ErrIntrouvable si le serveur n'a aucune affectation, ni dans le
// scénario, ni dans le réel, à retirer ; ErrValidation si dateFin est mal
// formée ou antérieure au début de la ligne retirée.
func (d *Depot) RetirerDuScenario(serveurID int64, scenarioID int64, dateFin string) error {
	if err := ValiderDate("date de fin", dateFin); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		return d.retirerDuScenarioTx(tx, serveurID, scenarioID, dateFin)
	})
}

// retirerDuScenarioTx est le corps de RetirerDuScenario, réutilisable dans
// une transaction plus large (Materialiser). dateFin est supposée validée.
func (d *Depot) retirerDuScenarioTx(tx *sql.Tx, serveurID int64, scenarioID int64, dateFin string) error {
	ctx := fmt.Sprintf("retrait du serveur %d du scénario %d", serveurID, scenarioID)
	{

		var ligneID int64
		var debut string
		err := tx.QueryRow(
			`SELECT id, date_debut FROM affectation WHERE serveur_id = ? AND scenario_id = ?
			 ORDER BY date_debut DESC, id DESC LIMIT 1`,
			serveurID, scenarioID).Scan(&ligneID, &debut)
		if err == sql.ErrNoRows {
			// pas encore de ligne dans ce scénario pour ce serveur : la calquer
			// sur l'affectation réelle active.
			var clusterID int64
			errReel := tx.QueryRow(
				`SELECT cluster_id, date_debut FROM affectation
				 WHERE serveur_id = ? AND scenario_id IS NULL AND date_fin IS NULL`,
				serveurID).Scan(&clusterID, &debut)
			if errReel == sql.ErrNoRows {
				return fmt.Errorf("%s : %w : aucune affectation active à retirer", ctx, ErrIntrouvable)
			}
			if errReel != nil {
				return fmt.Errorf("%s : %w", ctx, errReel)
			}
			if err := d.affecterTx(tx, serveurID, clusterID, debut, &scenarioID, nil); err != nil {
				return err
			}
			if err := tx.QueryRow(
				`SELECT id FROM affectation WHERE serveur_id = ? AND scenario_id = ? AND date_fin IS NULL`,
				serveurID, scenarioID).Scan(&ligneID); err != nil {
				return fmt.Errorf("%s : %w", ctx, err)
			}
		} else if err != nil {
			return fmt.Errorf("%s : %w", ctx, err)
		}

		if dateFin < debut {
			return fmt.Errorf("%s : %w : fin %s antérieure au début %s", ctx, ErrValidation, dateFin, debut)
		}
		return d.poserDateFin(tx, ctx, ligneID, dateFin)
	}
}

// ListerAffectationsServeur renvoie l'historique complet des affectations d'un
// serveur, tous seaux confondus, trié par date de début. C'est cette lecture qui
// garantit qu'un serveur réaffecté conserve toutes ses vies.
func (d *Depot) ListerAffectationsServeur(serveurID int64) ([]Affectation, error) {
	lignes, err := d.base.Query(
		`SELECT `+colonnesAffectation+` FROM affectation
		 WHERE serveur_id = ? ORDER BY date_debut, id`, serveurID)
	if err != nil {
		return nil, traduire(fmt.Sprintf("affectations du serveur %d", serveurID), err)
	}
	defer lignes.Close()

	var out []Affectation
	for lignes.Next() {
		a, err := scanAffectation(lignes)
		if err != nil {
			return nil, fmt.Errorf("affectations du serveur %d : %w", serveurID, err)
		}
		out = append(out, a)
	}
	return out, lignes.Err()
}

// ListerAffectationsCluster renvoie les affectations actives à aDate sur un
// cluster. Lecture par surcharge : le réel toujours, plus les affectations du
// scénario quand scenarioID est fourni (« surcharge du scénario, à défaut le
// réel »). Bornes inclusives des deux côtés.
func (d *Depot) ListerAffectationsCluster(clusterID int64, scenarioID *int64, aDate string) ([]Affectation, error) {
	if err := ValiderDate("date", aDate); err != nil {
		return nil, err
	}
	filtreScenario := "scenario_id IS NULL"
	args := []any{clusterID, aDate, aDate}
	if scenarioID != nil {
		filtreScenario = "(scenario_id IS NULL OR scenario_id = ?)"
		args = append(args, *scenarioID)
	}

	lignes, err := d.base.Query(
		`SELECT `+colonnesAffectation+` FROM affectation
		 WHERE cluster_id = ? AND date_debut <= ?
		   AND (date_fin IS NULL OR date_fin >= ?)
		   AND `+filtreScenario+`
		 ORDER BY date_debut, id`, args...)
	if err != nil {
		return nil, traduire(fmt.Sprintf("affectations du cluster %d", clusterID), err)
	}
	defer lignes.Close()

	var out []Affectation
	for lignes.Next() {
		a, err := scanAffectation(lignes)
		if err != nil {
			return nil, fmt.Errorf("affectations du cluster %d : %w", clusterID, err)
		}
		out = append(out, a)
	}
	return out, lignes.Err()
}

// AffectationsResolues renvoie, pour un cluster et une date donnés, les
// affectations *effectives* sous un scénario : contrairement à
// ListerAffectationsCluster qui ajoute simplement les lignes du scénario à
// celles du réel (utile pour lister les deltas bruts), AffectationsResolues
// applique la même règle de surcharge que ContraintesResolues et
// ResoudreValeur — pour chaque serveur, la ligne active du scénario, si elle
// existe, l'emporte *entièrement* sur celle du réel. Un serveur déplacé ou
// retiré par le scénario n'apparaît donc plus dans le réel pour ce cluster :
// c'est ce qui rend une hypothèse lisible dans un écran unique (§ point de
// vigilance ergonomique de v2.1 dans docs/backlog.md), sans qu'un même serveur
// semble occuper deux clusters à la fois.
//
// « Retirer » un serveur du cluster dans un scénario, sans le réaffecter
// ailleurs, se fait en ouvrant puis en refermant une affectation dans le seau
// du scénario (Affecter puis Desaffecter avec le même scenarioID) : le
// scénario porte alors une ligne pour ce serveur qui n'est active à aucune
// date, ce qui suffit à faire gagner la surcharge (aucune ligne active dans
// le scénario à aDate, mais au moins une ligne existe : voir la note dans la
// boucle ci-dessous — un serveur avec une ligne de scénario non active à
// aDate n'a *aucune* affectation effective, le réel ne reprend pas la main).
//
// scenarioID nil renvoie exactement ListerAffectationsCluster(clusterID, nil, aDate).
func (d *Depot) AffectationsResolues(clusterID int64, scenarioID *int64, aDate string) ([]Affectation, error) {
	if err := ValiderDate("date", aDate); err != nil {
		return nil, err
	}
	if scenarioID == nil {
		return d.ListerAffectationsCluster(clusterID, nil, aDate)
	}

	reel, err := actifsParServeur(d.base, nil, aDate)
	if err != nil {
		return nil, fmt.Errorf("affectations résolues du cluster %d : %w", clusterID, err)
	}
	scenario, err := d.affectationsTouchees(*scenarioID)
	if err != nil {
		return nil, fmt.Errorf("affectations résolues du cluster %d : %w", clusterID, err)
	}
	scenarioActif, err := actifsParServeur(d.base, scenarioID, aDate)
	if err != nil {
		return nil, fmt.Errorf("affectations résolues du cluster %d : %w", clusterID, err)
	}

	var out []Affectation
	for serveurID := range reel {
		effective, existeSurcharge := scenarioActif[serveurID]
		_, toucheParScenario := scenario[serveurID]
		switch {
		case existeSurcharge:
			if effective.ClusterID == clusterID {
				out = append(out, effective)
			}
		case toucheParScenario:
			// le scénario porte une ligne pour ce serveur mais aucune n'est
			// active à aDate : retrait effectif, le réel ne reprend pas la
			// main (voir la note de godoc ci-dessus).
		default:
			if reel[serveurID].ClusterID == clusterID {
				out = append(out, reel[serveurID])
			}
		}
	}
	// serveurs que le scénario affecte à ce cluster sans qu'ils aient de
	// ligne réelle active à aDate (ajout pur, ou serveur hypothétique).
	for serveurID, effective := range scenarioActif {
		if _, dejaVuReel := reel[serveurID]; dejaVuReel {
			continue
		}
		if effective.ClusterID == clusterID {
			out = append(out, effective)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ServeurID < out[j].ServeurID })
	return out, nil
}

// AffectationEffectiveServeur renvoie l'affectation effective d'*un* serveur
// à une date, sous un scénario — même règle de surcharge qu'AffectationsResolues,
// mais sans avoir à connaître le cluster visé à l'avance (utile pour
// retrouver où un serveur est passé, plutôt que qui occupe un cluster donné).
// Renvoie (nil, nil) si le serveur n'a aucune affectation effective à cette
// date.
func (d *Depot) AffectationEffectiveServeur(serveurID int64, scenarioID *int64, aDate string) (*Affectation, error) {
	if err := ValiderDate("date", aDate); err != nil {
		return nil, err
	}
	if scenarioID != nil {
		touche, err := d.serveurToucheParScenario(serveurID, *scenarioID)
		if err != nil {
			return nil, err
		}
		if touche {
			return affectationActiveServeur(d.base, serveurID, scenarioID, aDate)
		}
	}
	return affectationActiveServeur(d.base, serveurID, nil, aDate)
}

// serveurToucheParScenario indique si un serveur a au moins une ligne
// d'affectation dans le seau du scénario — voir la godoc d'AffectationsResolues.
func (d *Depot) serveurToucheParScenario(serveurID, scenarioID int64) (bool, error) {
	var n int
	err := d.base.QueryRow(
		`SELECT COUNT(*) FROM affectation WHERE serveur_id = ? AND scenario_id = ?`,
		serveurID, scenarioID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("détection de surcharge du serveur %d : %w", serveurID, err)
	}
	return n > 0, nil
}

// affectationActiveServeur renvoie l'affectation active à aDate d'un serveur
// dans le seau désigné par scenarioID (nil = réel), ou (nil, nil) s'il n'y en
// a aucune.
func affectationActiveServeur(c conn, serveurID int64, scenarioID *int64, aDate string) (*Affectation, error) {
	requete := `SELECT ` + colonnesAffectation + ` FROM affectation
		WHERE serveur_id = ? AND date_debut <= ? AND (date_fin IS NULL OR date_fin >= ?) AND `
	args := []any{serveurID, aDate, aDate}
	if scenarioID != nil {
		requete += `scenario_id = ?`
		args = append(args, *scenarioID)
	} else {
		requete += `scenario_id IS NULL`
	}

	a, err := scanAffectation(c.QueryRow(requete, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("affectation effective du serveur %d : %w", serveurID, err)
	}
	return &a, nil
}

// actifsParServeur renvoie, pour le seau désigné par scenarioID (nil = réel),
// l'affectation active à aDate de chaque serveur qui en a une.
func actifsParServeur(base conn, scenarioID *int64, aDate string) (map[int64]Affectation, error) {
	filtreScenario := "scenario_id IS NULL"
	args := []any{aDate, aDate}
	if scenarioID != nil {
		filtreScenario = "scenario_id = ?"
		args = []any{*scenarioID, aDate, aDate}
	}
	lignes, err := base.Query(
		`SELECT `+colonnesAffectation+` FROM affectation
		 WHERE `+filtreScenario+` AND date_debut <= ? AND (date_fin IS NULL OR date_fin >= ?)`,
		args...)
	if err != nil {
		return nil, err
	}
	defer lignes.Close()

	out := map[int64]Affectation{}
	for lignes.Next() {
		a, err := scanAffectation(lignes)
		if err != nil {
			return nil, err
		}
		out[a.ServeurID] = a // invariant 3 : au plus une par seau
	}
	return out, lignes.Err()
}

// affectationsTouchees renvoie l'ensemble des serveurs qui ont au moins une
// ligne d'affectation dans le seau du scénario, active ou non — c'est ce qui
// détermine si la surcharge s'applique à ce serveur (voir AffectationsResolues).
func (d *Depot) affectationsTouchees(scenarioID int64) (map[int64]struct{}, error) {
	lignes, err := d.base.Query(
		`SELECT DISTINCT serveur_id FROM affectation WHERE scenario_id = ?`, scenarioID)
	if err != nil {
		return nil, err
	}
	defer lignes.Close()

	out := map[int64]struct{}{}
	for lignes.Next() {
		var id int64
		if err := lignes.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, lignes.Err()
}

// HistoriqueServeur renvoie la suite chronologique des vies d'un serveur (chaque
// affectation, avec le cluster visé), en lecture par surcharge : le réel, plus
// le scénario si scenarioID est fourni. Trié par date de début.
func (d *Depot) HistoriqueServeur(serveurID int64, scenarioID *int64) ([]VieServeur, error) {
	filtreScenario := "a.scenario_id IS NULL"
	args := []any{serveurID}
	if scenarioID != nil {
		filtreScenario = "(a.scenario_id IS NULL OR a.scenario_id = ?)"
		args = append(args, *scenarioID)
	}

	lignes, err := d.base.Query(
		`SELECT `+prefixer("a.", colonnesAffectation)+`, c.nom, c.projet_id
		 FROM affectation a
		 JOIN cluster c ON c.id = a.cluster_id
		 WHERE a.serveur_id = ? AND `+filtreScenario+`
		 ORDER BY a.date_debut, a.id`, args...)
	if err != nil {
		return nil, traduire(fmt.Sprintf("historique du serveur %d", serveurID), err)
	}
	defer lignes.Close()

	var out []VieServeur
	for lignes.Next() {
		var v VieServeur
		if err := lignes.Scan(&v.ID, &v.ServeurID, &v.ClusterID, &v.DateDebut,
			&v.DateFin, &v.ScenarioID, &v.Commentaire,
			&v.ClusterNom, &v.ClusterProjetID); err != nil {
			return nil, fmt.Errorf("historique du serveur %d : %w", serveurID, err)
		}
		out = append(out, v)
	}
	return out, lignes.Err()
}
