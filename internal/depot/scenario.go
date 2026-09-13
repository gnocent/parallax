package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Statuts d'un scénario.
const (
	ScenarioBrouillon = "BROUILLON"
	ScenarioActif     = "ACTIF"
	ScenarioRetenu    = "RETENU"
	ScenarioAbandonne = "ABANDONNE"
)

// Scenario est un calque de deltas posé sur le réel. scenario_id IS NULL
// désigne le réel ; un scénario ne duplique jamais les données du réel, il ne
// porte que ses surcharges (variables, contraintes) et ses ajouts/retraits
// (serveurs, affectations).
type Scenario struct {
	ID           int64
	Nom          string
	ProjetID     *int64 // rattachement pour le classement, pas une restriction
	Description  string // obligatoire : on y revient des mois après
	Statut       string
	DateCreation string
	DateCloture  *string
	AuteurID     *int64
}

// CreerScenario insère un scénario en statut BROUILLON. La description est
// obligatoire et non vide.
//
// Erreurs : ErrValidation si nom ou description vide ; ErrReference si le
// projet ou l'auteur n'existe pas.
func (d *Depot) CreerScenario(s Scenario) (Scenario, error) {
	s.Nom = strings.TrimSpace(s.Nom)
	s.Description = strings.TrimSpace(s.Description)
	if s.Nom == "" {
		return Scenario{}, fmt.Errorf("création d'un scénario : %w : nom vide", ErrValidation)
	}
	if s.Description == "" {
		return Scenario{}, fmt.Errorf(
			"création d'un scénario : %w : description obligatoire", ErrValidation)
	}

	s.Statut = ScenarioBrouillon
	s.DateCreation = Horodatage()
	return creerJournalise(d, "scenario", func(tx *sql.Tx) (Scenario, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO scenario (nom, projet_id, description, statut, date_creation, auteur_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			s.Nom, s.ProjetID, s.Description, s.Statut, s.DateCreation, s.AuteurID)
		if err != nil {
			return Scenario{}, 0, traduire("création du scénario "+s.Nom, err)
		}
		s.ID, _ = res.LastInsertId()
		return s, s.ID, nil
	})
}

// LireScenario renvoie un scénario par identifiant, ou ErrIntrouvable.
func (d *Depot) LireScenario(id int64) (Scenario, error) {
	return lireScenario(d.base, id)
}

// ListerScenarios renvoie les scénarios, les clos (RETENU, ABANDONNE) exclus
// sauf si inclureClos est vrai. Tri par date de création décroissante.
func (d *Depot) ListerScenarios(inclureClos bool) ([]Scenario, error) {
	requete := `SELECT id, nom, projet_id, description, statut, date_creation,
	                   date_cloture, auteur_id
	            FROM scenario`
	if !inclureClos {
		requete += ` WHERE statut IN ('BROUILLON', 'ACTIF')`
	}
	requete += ` ORDER BY date_creation DESC, id DESC`

	lignes, err := d.base.Query(requete)
	if err != nil {
		return nil, traduire("liste des scénarios", err)
	}
	defer lignes.Close()

	var out []Scenario
	for lignes.Next() {
		s, err := scanScenario(lignes)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, lignes.Err()
}

// ModifierScenario met à jour le nom, la description et le rattachement projet.
// Le statut se pilote par ActiverScenario, AbandonnerScenario et Promouvoir.
func (d *Depot) ModifierScenario(s Scenario) error {
	s.Nom = strings.TrimSpace(s.Nom)
	s.Description = strings.TrimSpace(s.Description)
	if s.Nom == "" || s.Description == "" {
		return fmt.Errorf("modification du scénario %d : %w : nom ou description vide",
			s.ID, ErrValidation)
	}
	return modifierJournalise(d, "scenario", s.ID, ActionModification, lireScenario, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE scenario SET nom = ?, description = ?, projet_id = ? WHERE id = ?`,
			s.Nom, s.Description, s.ProjetID, s.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du scénario %d", s.ID), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("modification du scénario %d", s.ID))
	})
}

// ActiverScenario fait passer un scénario de BROUILLON à ACTIF.
func (d *Depot) ActiverScenario(id int64) error {
	return d.changerStatutScenario(id, ScenarioActif, false,
		ScenarioBrouillon, ScenarioActif)
}

// AbandonnerScenario clôt un scénario sans rien détruire : ses deltas restent
// en base, simplement invisibles hors consultation explicite. On peut toujours
// y revenir en le rouvrant (ReprendreScenario).
func (d *Depot) AbandonnerScenario(id int64) error {
	return d.changerStatutScenario(id, ScenarioAbandonne, true,
		ScenarioBrouillon, ScenarioActif, ScenarioAbandonne)
}

// ReprendreScenario rouvre un scénario abandonné en ACTIF : le cycle
// hypothèse / abandon / reprise se fait sans aucune ressaisie.
func (d *Depot) ReprendreScenario(id int64) error {
	return d.changerStatutScenario(id, ScenarioActif, false, ScenarioAbandonne)
}

// Promouvoir bascule les deltas d'un scénario dans le réel, en y appliquant
// la même règle de surcharge que la lecture (modele-donnees.md §6) — pas un
// simple passage de scenario_id à NULL, qui laisserait un serveur déplacé
// avec deux affectations ouvertes (refusé par idx_affectation_active_unique)
// et une variable surchargée avec deux valeurs réelles à la même portée :
//
//   - affectations (promouvoirAffectations) : l'affectation réelle courante
//     d'un serveur déplacé est close la veille du déplacement ; celle d'un
//     serveur retiré prend la date de fin du retrait ; les lignes du scénario
//     rejoignent ensuite le réel, après contrôle de non-chevauchement ;
//   - variables et contraintes : une surcharge à une portée où le réel a déjà
//     une valeur écrase cette valeur (historisée pour les variables) plutôt
//     que de la doubler ; sinon la ligne rejoint le réel ;
//   - serveurs hypothétiques : passent COMMANDE dans le réel.
//
// Le scénario passe RETENU, les scénarios listés dans abandonner sont clos en
// ABANDONNE. Cette liste est fournie par l'appelant : aucune règle implicite
// ne décide à sa place quels scénarios deviennent caducs.
//
// Rien n'est détruit qui soit une donnée : les vies antérieures restent dans
// l'historique, closes et non effacées (invariant 6 du §12). Seuls
// disparaissent les artefacts techniques devenus redondants — la ligne clone
// d'un retrait, fusionnée dans l'affectation réelle qu'elle recopiait, et une
// surcharge de valeur fusionnée dans la valeur réelle qu'elle remplace.
//
// Tout ou rien : la moindre erreur annule la transaction entière.
//
// Erreurs : ErrIntrouvable si le scénario n'existe pas ; ErrValidation s'il
// n'est pas dans un statut promouvable (BROUILLON ou ACTIF) ou si un
// concurrent listé est absent ou déjà clos ; ErrChevauchement si un
// déplacement est daté au plus tard le début de l'affectation réelle
// courante, ou recouvre une vie réelle passée — impossible sans réécrire
// l'histoire, donc refusé en nommant le serveur.
func (d *Depot) Promouvoir(scenarioID int64, abandonner []int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		s, err := lireScenario(tx, scenarioID)
		if err != nil {
			return err
		}
		if s.Statut != ScenarioBrouillon && s.Statut != ScenarioActif {
			return fmt.Errorf("promotion du scénario %d : %w : statut %s non promouvable",
				scenarioID, ErrValidation, s.Statut)
		}

		// Serveurs hypothétiques → COMMANDE et rattachement au réel, dans le
		// même ordre : le CHECK « HYPOTHESE ⇒ scénario » est réévalué après
		// mise à jour de la ligne, donc statut et scenario_id doivent bouger
		// ensemble.
		// Une ligne de journal par serveur promu : la promotion est le geste
		// composite qu'on relit le plus souvent après coup.
		serveurs, err := identifiants(tx, `SELECT id FROM serveur WHERE scenario_id = ? ORDER BY id`, scenarioID)
		if err != nil {
			return fmt.Errorf("promotion : serveurs du scénario : %w", err)
		}
		for _, id := range serveurs {
			if err := d.ecrireJournalise(tx, "serveur", "serveur", id, ActionModification,
				"promotion : serveurs hypothétiques",
				`UPDATE serveur SET statut = CASE WHEN statut = 'HYPOTHESE' THEN 'COMMANDE' ELSE statut END,
				        scenario_id = NULL
				 WHERE id = ?`, id); err != nil {
				return err
			}
		}
		if err := d.promouvoirAffectations(tx, scenarioID); err != nil {
			return err
		}
		if err := d.promouvoirVariables(tx, scenarioID); err != nil {
			return err
		}
		if err := d.promouvoirContraintes(tx, scenarioID); err != nil {
			return err
		}

		if err := d.ecrireJournalise(tx, "scenario", "scenario", scenarioID, ActionModification,
			"promotion : clôture du scénario retenu",
			`UPDATE scenario SET statut = 'RETENU', date_cloture = ? WHERE id = ?`,
			Horodatage(), scenarioID); err != nil {
			return err
		}

		for _, id := range abandonner {
			if id == scenarioID {
				continue
			}
			avant, err := lireScenario(tx, id)
			if err != nil {
				return fmt.Errorf("promotion : %w : scénario concurrent %d absent ou déjà clos", ErrValidation, id)
			}
			res, err := tx.Exec(
				`UPDATE scenario SET statut = 'ABANDONNE', date_cloture = ?
				 WHERE id = ? AND statut IN ('BROUILLON', 'ACTIF')`,
				Horodatage(), id)
			if err != nil {
				return traduire(fmt.Sprintf("promotion : abandon du scénario %d", id), err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf(
					"promotion : %w : scénario concurrent %d absent ou déjà clos",
					ErrValidation, id)
			}
			apres, err := lireScenario(tx, id)
			if err != nil {
				return err
			}
			if err := d.journaliser(tx, "scenario", id, ActionModification, avant, apres); err != nil {
				return err
			}
		}
		return nil
	})
}

// identifiants exécute une requête ne renvoyant qu'une colonne d'entiers.
func identifiants(c conn, requete string, args ...any) ([]int64, error) {
	lignes, err := c.Query(requete, args...)
	if err != nil {
		return nil, err
	}
	defer lignes.Close()
	var out []int64
	for lignes.Next() {
		var id int64
		if err := lignes.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, lignes.Err()
}

// ------------------------------------------------------ promotion des deltas

// ligneAffectationPromue est le sous-ensemble d'une affectation du scénario
// nécessaire à sa promotion.
type ligneAffectationPromue struct {
	id        int64
	clusterID int64
	debut     string
	fin       *string
}

// promouvoirAffectations applique au réel, serveur par serveur, la règle de
// surcharge du §6 pour les affectations du scénario. Voir Promouvoir.
func (d *Depot) promouvoirAffectations(tx *sql.Tx, scenarioID int64) error {
	lignes, err := tx.Query(
		`SELECT id, serveur_id, cluster_id, date_debut, date_fin FROM affectation
		 WHERE scenario_id = ? ORDER BY serveur_id, date_debut, id`, scenarioID)
	if err != nil {
		return traduire("promotion : affectations du scénario", err)
	}
	parServeur := map[int64][]ligneAffectationPromue{}
	var ordre []int64
	for lignes.Next() {
		var l ligneAffectationPromue
		var serveurID int64
		if err := lignes.Scan(&l.id, &serveurID, &l.clusterID, &l.debut, &l.fin); err != nil {
			lignes.Close()
			return fmt.Errorf("promotion : affectations du scénario : %w", err)
		}
		if _, vu := parServeur[serveurID]; !vu {
			ordre = append(ordre, serveurID)
		}
		parServeur[serveurID] = append(parServeur[serveurID], l)
	}
	lignes.Close()
	if err := lignes.Err(); err != nil {
		return err
	}

	for _, serveurID := range ordre {
		if err := d.promouvoirAffectationsServeur(tx, serveurID, parServeur[serveurID]); err != nil {
			return err
		}
	}
	return nil
}

func (d *Depot) promouvoirAffectationsServeur(tx *sql.Tx, serveurID int64, deltas []ligneAffectationPromue) error {
	ctx := fmt.Sprintf("promotion : serveur %d", serveurID)

	// l'affectation réelle courante, s'il y en a une
	var reel *ligneAffectationPromue
	var r ligneAffectationPromue
	err := tx.QueryRow(
		`SELECT id, cluster_id, date_debut FROM affectation
		 WHERE serveur_id = ? AND scenario_id IS NULL AND date_fin IS NULL`,
		serveurID).Scan(&r.id, &r.clusterID, &r.debut)
	switch {
	case err == nil:
		reel = &r
	case err == sql.ErrNoRows:
	default:
		return fmt.Errorf("%s : %w", ctx, err)
	}

	// 1. Une ligne du scénario qui recopie l'affectation réelle courante (même
	//    cluster, même début) est le clone posé par RetirerDuScenario : sa date
	//    de fin est le retrait lui-même. Elle se fusionne dans la ligne réelle
	//    plutôt que de la doubler.
	var restantes []ligneAffectationPromue
	reelClos := false
	for _, s := range deltas {
		if reel != nil && !reelClos && s.clusterID == reel.clusterID && s.debut == reel.debut {
			if s.fin != nil {
				if err := d.ecrireJournalise(tx, "affectation", "affectation", reel.id, ActionModification,
					ctx+" : clôture du retrait",
					`UPDATE affectation SET date_fin = ? WHERE id = ?`, *s.fin, reel.id); err != nil {
					return err
				}
				reelClos = true
			}
			if err := d.ecrireJournalise(tx, "affectation", "affectation", s.id, ActionSuppression,
				ctx+" : fusion du clone de retrait",
				`DELETE FROM affectation WHERE id = ?`, s.id); err != nil {
				return err
			}
			continue
		}
		restantes = append(restantes, s)
	}

	// 2. Un déplacement clôt l'affectation réelle courante la veille de la
	//    première ligne du scénario — sauf si cela réécrirait l'histoire.
	if reel != nil && !reelClos && len(restantes) > 0 {
		veille, err := dateMoinsUnJour(restantes[0].debut)
		if err != nil {
			return err
		}
		if veille < reel.debut {
			return fmt.Errorf(
				"%s : %w : déplacement daté du %s, au plus tard le début de l'affectation réelle courante (%s)",
				ctx, ErrChevauchement, restantes[0].debut, reel.debut)
		}
		if err := d.ecrireJournalise(tx, "affectation", "affectation", reel.id, ActionModification,
			ctx+" : clôture avant déplacement",
			`UPDATE affectation SET date_fin = ? WHERE id = ?`, veille, reel.id); err != nil {
			return err
		}
	}

	// 3. Les lignes restantes rejoignent le réel, chacune après contrôle
	//    qu'elle ne recouvre aucune vie réelle (y compris celle qu'on vient de
	//    clore : sa fin est la veille, donc pas de recouvrement).
	for _, s := range restantes {
		fin := "9999-12-31"
		if s.fin != nil {
			fin = *s.fin
		}
		var recouvre int
		if err := tx.QueryRow(
			`SELECT COUNT(*) FROM affectation
			 WHERE serveur_id = ? AND scenario_id IS NULL AND id != ?
			   AND date_debut <= ? AND (date_fin IS NULL OR date_fin >= ?)`,
			serveurID, s.id, fin, s.debut).Scan(&recouvre); err != nil {
			return fmt.Errorf("%s : %w", ctx, err)
		}
		if recouvre > 0 {
			return fmt.Errorf("%s : %w : l'affectation du scénario à partir du %s recouvre une vie réelle",
				ctx, ErrChevauchement, s.debut)
		}
		if err := d.ecrireJournalise(tx, "affectation", "affectation", s.id, ActionModification,
			ctx+" : passage dans le réel",
			`UPDATE affectation SET scenario_id = NULL WHERE id = ?`, s.id); err != nil {
			return err
		}
	}
	return nil
}

// promouvoirVariables fait rejoindre le réel aux surcharges de variables du
// scénario. Une surcharge à une portée où le réel a déjà une valeur l'écrase
// (avec historisation, comme DefinirValeur) et disparaît, son historique
// propre rattaché à la valeur réelle ; sinon la ligne passe simplement dans
// le réel.
func (d *Depot) promouvoirVariables(tx *sql.Tx, scenarioID int64) error {
	lignes, err := tx.Query(
		`SELECT id, variable_id, annee, projet_id, environnement_id, techno_id, tier_id,
		        cluster_id, valeur, commentaire
		 FROM variable_valeur WHERE scenario_id = ?`, scenarioID)
	if err != nil {
		return traduire("promotion : variables du scénario", err)
	}
	var surcharges []ValeurVariable
	for lignes.Next() {
		var v ValeurVariable
		if err := lignes.Scan(&v.ID, &v.VariableID, &v.Annee, &v.Portee.ProjetID,
			&v.Portee.EnvironnementID, &v.Portee.TechnoID, &v.Portee.TierID,
			&v.Portee.ClusterID, &v.Valeur, &v.Commentaire); err != nil {
			lignes.Close()
			return fmt.Errorf("promotion : variables du scénario : %w", err)
		}
		surcharges = append(surcharges, v)
	}
	lignes.Close()
	if err := lignes.Err(); err != nil {
		return err
	}

	for _, v := range surcharges {
		ctx := fmt.Sprintf("promotion : variable %d, année %d", v.VariableID, v.Annee)
		clause, args := scopeWhereValeur(v.VariableID, nil, v.Annee, v.Portee)
		var reelID int64
		var ancienne float64
		err := tx.QueryRow(`SELECT id, valeur FROM variable_valeur WHERE `+clause, args...).Scan(&reelID, &ancienne)
		switch {
		case err == sql.ErrNoRows:
			if err := d.ecrireJournalise(tx, "variable_valeur", "variable_valeur", v.ID, ActionModification, ctx,
				`UPDATE variable_valeur SET scenario_id = NULL WHERE id = ?`, v.ID); err != nil {
				return err
			}
		case err != nil:
			return fmt.Errorf("%s : %w", ctx, err)
		default:
			if ancienne != v.Valeur {
				if _, err := tx.Exec(
					`INSERT INTO variable_valeur_historique (valeur_id, ancienne, nouvelle, horodatage, utilisateur_id)
					 VALUES (?, ?, ?, ?, NULL)`, reelID, ancienne, v.Valeur, Horodatage()); err != nil {
					return traduire(ctx+" : historisation", err)
				}
			}
			if err := d.ecrireJournalise(tx, "variable_valeur", "variable_valeur", reelID, ActionModification, ctx+" : écrasement",
				`UPDATE variable_valeur SET valeur = ?, commentaire = ?, modifie_le = ? WHERE id = ?`,
				v.Valeur, v.Commentaire, Horodatage(), reelID); err != nil {
				return err
			}
			if _, err := tx.Exec(
				`UPDATE variable_valeur_historique SET valeur_id = ? WHERE valeur_id = ?`, reelID, v.ID); err != nil {
				return traduire(ctx+" : rattachement de l'historique", err)
			}
			if err := d.ecrireJournalise(tx, "variable_valeur", "variable_valeur", v.ID, ActionSuppression, ctx+" : fusion de la surcharge",
				`DELETE FROM variable_valeur WHERE id = ?`, v.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// promouvoirContraintes : même geste que promouvoirVariables pour les
// contraintes, clé (cluster, année, type). Pas d'historique sur les
// contraintes, donc pas d'historisation.
func (d *Depot) promouvoirContraintes(tx *sql.Tx, scenarioID int64) error {
	lignes, err := tx.Query(
		`SELECT id, cluster_id, annee, type, valeur, portee, commentaire
		 FROM contrainte WHERE scenario_id = ?`, scenarioID)
	if err != nil {
		return traduire("promotion : contraintes du scénario", err)
	}
	var surcharges []Contrainte
	for lignes.Next() {
		var c Contrainte
		if err := lignes.Scan(&c.ID, &c.ClusterID, &c.Annee, &c.Type, &c.Valeur, &c.Portee, &c.Commentaire); err != nil {
			lignes.Close()
			return fmt.Errorf("promotion : contraintes du scénario : %w", err)
		}
		surcharges = append(surcharges, c)
	}
	lignes.Close()
	if err := lignes.Err(); err != nil {
		return err
	}

	for _, c := range surcharges {
		ctx := fmt.Sprintf("promotion : contrainte %s du cluster %d, année %d", c.Type, c.ClusterID, c.Annee)
		var reelID int64
		err := tx.QueryRow(
			`SELECT id FROM contrainte
			 WHERE cluster_id = ? AND annee = ? AND type = ? AND scenario_id IS NULL`,
			c.ClusterID, c.Annee, c.Type).Scan(&reelID)
		switch {
		case err == sql.ErrNoRows:
			if err := d.ecrireJournalise(tx, "contrainte", "contrainte", c.ID, ActionModification, ctx,
				`UPDATE contrainte SET scenario_id = NULL WHERE id = ?`, c.ID); err != nil {
				return err
			}
		case err != nil:
			return fmt.Errorf("%s : %w", ctx, err)
		default:
			if err := d.ecrireJournalise(tx, "contrainte", "contrainte", reelID, ActionModification, ctx+" : écrasement",
				`UPDATE contrainte SET valeur = ?, portee = ?, commentaire = ? WHERE id = ?`,
				c.Valeur, c.Portee, c.Commentaire, reelID); err != nil {
				return err
			}
			if err := d.ecrireJournalise(tx, "contrainte", "contrainte", c.ID, ActionSuppression, ctx+" : fusion de la surcharge",
				`DELETE FROM contrainte WHERE id = ?`, c.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// --------------------------------------------------------------- helpers

// changerStatutScenario pose le statut cible seulement si le statut courant
// figure dans depuis. date_cloture est renseignée quand poserCloture est vrai.
func (d *Depot) changerStatutScenario(id int64, cible string, poserCloture bool,
	depuis ...string) error {

	marques := make([]string, len(depuis))
	args := []any{cible}
	if poserCloture {
		args = append(args, Horodatage())
	}
	args = append(args, id)
	for i, s := range depuis {
		marques[i] = "?"
		args = append(args, s)
	}

	set := "statut = ?, date_cloture = NULL"
	if poserCloture {
		set = "statut = ?, date_cloture = ?"
	}
	requete := fmt.Sprintf(
		`UPDATE scenario SET %s WHERE id = ? AND statut IN (%s)`,
		set, strings.Join(marques, ", "))

	return d.enTx(func(tx *sql.Tx) error {
		avant, err := lireScenario(tx, id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(requete, args...)
		if err != nil {
			return traduire(fmt.Sprintf("changement de statut du scénario %d", id), err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf(
				"changement de statut du scénario %d : %w : transition vers %s interdite depuis le statut courant",
				id, ErrValidation, cible)
		}
		apres, err := lireScenario(tx, id)
		if err != nil {
			return err
		}
		return d.journaliser(tx, "scenario", id, ActionModification, avant, apres)
	})
}

func lireScenario(c conn, id int64) (Scenario, error) {
	var s Scenario
	err := scanUn(
		c.QueryRow(`SELECT id, nom, projet_id, description, statut, date_creation,
		                   date_cloture, auteur_id
		            FROM scenario WHERE id = ?`, id),
		fmt.Sprintf("lecture du scénario %d", id),
		&s.ID, &s.Nom, &s.ProjetID, &s.Description, &s.Statut,
		&s.DateCreation, &s.DateCloture, &s.AuteurID)
	return s, err
}

type scanneur interface {
	Scan(dest ...any) error
}

func scanScenario(s scanneur) (Scenario, error) {
	var sc Scenario
	err := s.Scan(&sc.ID, &sc.Nom, &sc.ProjetID, &sc.Description, &sc.Statut,
		&sc.DateCreation, &sc.DateCloture, &sc.AuteurID)
	if err != nil {
		return Scenario{}, fmt.Errorf("lecture d'un scénario : %w", err)
	}
	return sc, nil
}
