package depot

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Statuts d'un serveur, dans l'ordre du cycle de vie décrit au cadrage :
// une hypothèse est enrichie jusqu'à la réception, puis décommissionnée.
const (
	StatutHypothese      = "HYPOTHESE"
	StatutCommande       = "COMMANDE"
	StatutEnService      = "EN_SERVICE"
	StatutDecommissionne = "DECOMMISSIONNE"
)

// Serveur est l'unité matérielle. Son identité est la clé technique ID ; le nom
// physique n'est qu'un attribut (un serveur rendu puis récupéré peut redevenir
// une entité distincte). La quasi-totalité des colonnes est documentaire et
// nullable, d'où les pointeurs.
//
// scenario_id NULL désigne le réel. Invariant 5 : un serveur de statut
// HYPOTHESE porte obligatoirement un scenario_id ; la contrainte est posée en
// base (CHECK) et traduite en ErrValidation.
type Serveur struct {
	ID                int64
	PhysicalName      *string
	Hostname          *string
	SerialNumber      *string
	ZoneID            *int64
	PositionZone      *string
	IP                *string
	VlanID            *int64
	VersionOS         *string
	Typologie         *string
	CodeAppli         *string
	DemandeRef        *string
	DemandeServeurRef *string
	Statut            string
	ScenarioID        *int64
	DateEntree        *string
	DateSortie        *string
	Commentaire       *string
}

// FiltreServeur restreint ListerServeurs. Champs facultatifs : nil ne restreint
// pas. Pour ScenarioID, la sémantique est « le réel seulement si nil » : nil ne
// renvoie que les serveurs réels (scenario_id IS NULL) ; une valeur renvoie le
// réel plus les serveurs de ce scénario.
type FiltreServeur struct {
	Statut     *string
	ZoneID     *int64
	ScenarioID *int64
}

// ServeurSansAffectation associe un serveur candidat à la réutilisation à la
// date de fin de lease de sa révision courante (nil si achat, modèle sans
// lease, ou aucun rattachement de révision ouvert).
type ServeurSansAffectation struct {
	Serveur  Serveur
	FinLease *string
}

const colonnesServeur = `id, physical_name, hostname, serial_number, zone_id,
	position_zone, ip, vlan_id, version_os, typologie, code_appli,
	demande_ref, demande_serveur_ref, statut, scenario_id, date_entree, date_sortie, commentaire`

// ciblesServeur renvoie la liste des pointeurs de scan alignée sur
// colonnesServeur.
func ciblesServeur(s *Serveur) []any {
	return []any{
		&s.ID, &s.PhysicalName, &s.Hostname, &s.SerialNumber, &s.ZoneID,
		&s.PositionZone, &s.IP, &s.VlanID, &s.VersionOS, &s.Typologie, &s.CodeAppli,
		&s.DemandeRef, &s.DemandeServeurRef, &s.Statut, &s.ScenarioID, &s.DateEntree,
		&s.DateSortie, &s.Commentaire,
	}
}

func statutValide(statut string) bool {
	switch statut {
	case StatutHypothese, StatutCommande, StatutEnService, StatutDecommissionne:
		return true
	}
	return false
}

// Typologies attendues par l'outil interne de demande (backlog v3.2,
// décision 2026-09-12) : une énumération fixe, saisie et jamais déduite.
// Contrôlée ici plutôt que par un CHECK — la colonne existait avant la
// décision et une migration ne peut pas ajouter de CHECK sous SQLite.
var Typologies = []string{"P", "A", "D", "PA", "AD", "PAD"}

// TypologieValide accepte nil ou l'une des valeurs de Typologies.
func TypologieValide(t *string) bool {
	if t == nil {
		return true
	}
	for _, v := range Typologies {
		if *t == v {
			return true
		}
	}
	return false
}

func erreurTypologie(contexte string, t *string) error {
	return fmt.Errorf("%s : %w : typologie « %s » inconnue (attendu : %s)",
		contexte, ErrValidation, *t, strings.Join(Typologies, ", "))
}

// CreerServeur insère un serveur et renvoie la ligne complète, identifiant
// attribué.
//
// Erreurs : ErrValidation si le statut ou la typologie est hors énumération,
// ou si un serveur HYPOTHESE est créé sans scénario (invariant 5) ;
// ErrReference si une clé étrangère (zone, vlan, scénario) désigne une ligne
// inexistante.
func (d *Depot) CreerServeur(s Serveur) (Serveur, error) {
	if !statutValide(s.Statut) {
		return Serveur{}, fmt.Errorf(
			"création d'un serveur : %w : statut « %s » inconnu", ErrValidation, s.Statut)
	}
	if !TypologieValide(s.Typologie) {
		return Serveur{}, erreurTypologie("création d'un serveur", s.Typologie)
	}
	if s.Statut == StatutHypothese && s.ScenarioID == nil {
		return Serveur{}, fmt.Errorf(
			"création d'un serveur : %w : un serveur HYPOTHESE exige un scénario (invariant 5)",
			ErrValidation)
	}

	return creerJournalise(d, "serveur", func(tx *sql.Tx) (Serveur, int64, error) {
		res, err := tx.Exec(
			`INSERT INTO serveur
			 (physical_name, hostname, serial_number, zone_id, position_zone, ip,
			  vlan_id, version_os, typologie, code_appli, demande_ref, demande_serveur_ref,
			  statut, scenario_id, date_entree, date_sortie, commentaire)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			s.PhysicalName, s.Hostname, s.SerialNumber, s.ZoneID, s.PositionZone, s.IP,
			s.VlanID, s.VersionOS, s.Typologie, s.CodeAppli, s.DemandeRef, s.DemandeServeurRef,
			s.Statut, s.ScenarioID, s.DateEntree, s.DateSortie, s.Commentaire)
		if err != nil {
			return Serveur{}, 0, traduire("création d'un serveur", err)
		}
		s.ID, _ = res.LastInsertId()
		return s, s.ID, nil
	})
}

// LireServeur renvoie un serveur par identifiant, ou ErrIntrouvable.
func (d *Depot) LireServeur(id int64) (Serveur, error) {
	return lireServeur(d.base, id)
}

// ModifierServeur met à jour les seuls attributs documentaires. Le statut passe
// par ChangerStatut, le scénario par DefinirScenario : trois gestes distincts
// pour trois natures de changement.
//
// Erreurs : ErrValidation si la typologie est hors énumération ;
// ErrIntrouvable si l'identifiant n'existe pas ; ErrReference si une clé
// étrangère est invalide.
func (d *Depot) ModifierServeur(s Serveur) error {
	if !TypologieValide(s.Typologie) {
		return erreurTypologie(fmt.Sprintf("modification du serveur %d", s.ID), s.Typologie)
	}
	return d.enTx(func(tx *sql.Tx) error { return d.modifierServeurTx(tx, s) })
}

// modifierServeurTx est le corps de ModifierServeur, réutilisable dans une
// transaction plus large (mise à jour en masse, v3.5). Journalisé.
func (d *Depot) modifierServeurTx(tx *sql.Tx, s Serveur) error {
	avant, err := lireServeur(tx, s.ID)
	if err != nil {
		return err
	}
	res, err := tx.Exec(
		`UPDATE serveur SET
		   physical_name = ?, hostname = ?, serial_number = ?, zone_id = ?,
		   position_zone = ?, ip = ?, vlan_id = ?, version_os = ?, typologie = ?,
		   code_appli = ?, demande_ref = ?, demande_serveur_ref = ?, commentaire = ?
		 WHERE id = ?`,
		s.PhysicalName, s.Hostname, s.SerialNumber, s.ZoneID, s.PositionZone,
		s.IP, s.VlanID, s.VersionOS, s.Typologie, s.CodeAppli, s.DemandeRef,
		s.DemandeServeurRef, s.Commentaire, s.ID)
	if err != nil {
		return traduire(fmt.Sprintf("modification du serveur %d", s.ID), err)
	}
	if err := exigerUneLigne(res, fmt.Sprintf("modification du serveur %d", s.ID)); err != nil {
		return err
	}
	apres, err := lireServeur(tx, s.ID)
	if err != nil {
		return err
	}
	return d.journaliser(tx, "serveur", s.ID, ActionModification, avant, apres)
}

// ChangerStatut fait progresser un serveur dans son cycle de vie. La valeur est
// validée contre l'énumération ; un passage à HYPOTHESE alors que le serveur n'a
// pas de scénario est refusé par le CHECK de la base (invariant 5), traduit en
// ErrValidation.
//
// Erreurs : ErrValidation (statut inconnu, invariant 5) ; ErrIntrouvable.
func (d *Depot) ChangerStatut(id int64, statut string) error {
	if !statutValide(statut) {
		return fmt.Errorf(
			"changement de statut du serveur %d : %w : statut « %s » inconnu",
			id, ErrValidation, statut)
	}
	return d.enTx(func(tx *sql.Tx) error { return d.changerStatutTx(tx, id, statut) })
}

// changerStatutTx est le corps de ChangerStatut (statut supposé valide),
// réutilisable dans une transaction plus large. Journalisé.
func (d *Depot) changerStatutTx(tx *sql.Tx, id int64, statut string) error {
	// Le décommissionnement libère l'adresse (backlog v3.1) : le champ est
	// vidé, l'adresse redevient disponible pour le pool global.
	requete := `UPDATE serveur SET statut = ? WHERE id = ?`
	if statut == StatutDecommissionne {
		requete = `UPDATE serveur SET statut = ?, ip = NULL WHERE id = ?`
	}
	avant, err := lireServeur(tx, id)
	if err != nil {
		return err
	}
	res, err := tx.Exec(requete, statut, id)
	if err != nil {
		return traduire(fmt.Sprintf("changement de statut du serveur %d", id), err)
	}
	if err := exigerUneLigne(res, fmt.Sprintf("changement de statut du serveur %d", id)); err != nil {
		return err
	}
	// Un serveur décommissionné sort du parc (revue 2026-09-12) : sans ça il
	// continuait de compter dans l'offre et les vues tant que son affectation
	// restait ouverte. La date de sortie est posée si absente, et son
	// affectation réelle comme son rattachement en cours sont clos à cette
	// date — jamais avant leur début. Le retour à un autre statut efface la
	// date de sortie ; les périodes closes restent à rouvrir à la main.
	if statut == StatutDecommissionne {
		if err := d.sortirDuParcTx(tx, id, avant.DateSortie); err != nil {
			return err
		}
	} else if avant.Statut == StatutDecommissionne {
		if _, err := tx.Exec(`UPDATE serveur SET date_sortie = NULL WHERE id = ?`, id); err != nil {
			return traduire(fmt.Sprintf("retour en service du serveur %d", id), err)
		}
	}
	apres, err := lireServeur(tx, id)
	if err != nil {
		return err
	}
	return d.journaliser(tx, "serveur", id, ActionModification, avant, apres)
}

// sortirDuParcTx pose la date de sortie (aujourd'hui si absente) et clôt à
// cette date l'affectation réelle et le rattachement de révision encore
// ouverts, au plus tôt à leur date de début. Chaque clôture est journalisée.
func (d *Depot) sortirDuParcTx(tx *sql.Tx, id int64, dateSortie *string) error {
	sortie := Horodatage()[:10]
	if dateSortie != nil && *dateSortie != "" {
		sortie = *dateSortie
	} else if _, err := tx.Exec(`UPDATE serveur SET date_sortie = ? WHERE id = ?`, sortie, id); err != nil {
		return traduire(fmt.Sprintf("date de sortie du serveur %d", id), err)
	}
	ctx := fmt.Sprintf("décommissionnement du serveur %d", id)

	var affID int64
	var affDebut string
	switch err := tx.QueryRow(
		`SELECT id, date_debut FROM affectation WHERE serveur_id = ? AND scenario_id IS NULL AND date_fin IS NULL`,
		id).Scan(&affID, &affDebut); {
	case err == nil:
		if err := d.poserDateFin(tx, ctx, affID, auPlusTot(sortie, affDebut)); err != nil {
			return err
		}
	case err == sql.ErrNoRows:
	default:
		return fmt.Errorf("%s : %w", ctx, err)
	}

	var rattID int64
	var rattDebut string
	switch err := tx.QueryRow(
		`SELECT id, date_debut FROM serveur_revision WHERE serveur_id = ? AND date_fin IS NULL`,
		id).Scan(&rattID, &rattDebut); {
	case err == nil:
		avant, err := instantane(tx, "serveur_revision", rattID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE serveur_revision SET date_fin = ? WHERE id = ?`, auPlusTot(sortie, rattDebut), rattID); err != nil {
			return traduire(ctx, err)
		}
		if err := d.journalModificationTable(tx, "serveur_revision", "serveur_revision", rattID, ActionModification, avant); err != nil {
			return err
		}
	case err == sql.ErrNoRows:
	default:
		return fmt.Errorf("%s : %w", ctx, err)
	}
	return nil
}

// auPlusTot renvoie date, ou debut si date lui est antérieure — une période
// ne se clôt jamais avant son ouverture.
func auPlusTot(date, debut string) string {
	if date < debut {
		return debut
	}
	return date
}

// DefinirScenario rattache un serveur à un scénario, ou le ramène dans le réel
// (scenarioID nil) — c'est le geste de promotion d'un scénario.
//
// Erreurs : ErrValidation si ôter le scénario laisserait un serveur HYPOTHESE
// sans scénario (invariant 5) ; ErrReference si le scénario n'existe pas ;
// ErrIntrouvable si le serveur n'existe pas.
func (d *Depot) DefinirScenario(id int64, scenarioID *int64) error {
	return modifierJournalise(d, "serveur", id, ActionModification, lireServeur, func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`UPDATE serveur SET scenario_id = ? WHERE id = ?`, scenarioID, id)
		if err != nil {
			return traduire(fmt.Sprintf("rattachement du serveur %d à un scénario", id), err)
		}
		return exigerUneLigne(res, fmt.Sprintf("rattachement du serveur %d à un scénario", id))
	})
}

// ListerServeurs renvoie les serveurs retenus par le filtre, triés par id.
func (d *Depot) ListerServeurs(f FiltreServeur) ([]Serveur, error) {
	var cond []string
	var args []any
	if f.Statut != nil {
		cond = append(cond, "statut = ?")
		args = append(args, *f.Statut)
	}
	if f.ZoneID != nil {
		cond = append(cond, "zone_id = ?")
		args = append(args, *f.ZoneID)
	}
	if f.ScenarioID == nil {
		cond = append(cond, "scenario_id IS NULL")
	} else {
		cond = append(cond, "(scenario_id IS NULL OR scenario_id = ?)")
		args = append(args, *f.ScenarioID)
	}

	requete := `SELECT ` + colonnesServeur + ` FROM serveur`
	if len(cond) > 0 {
		requete += ` WHERE ` + strings.Join(cond, " AND ")
	}
	requete += ` ORDER BY id`

	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des serveurs", err)
	}
	defer lignes.Close()

	var out []Serveur
	for lignes.Next() {
		var s Serveur
		if err := lignes.Scan(ciblesServeur(&s)...); err != nil {
			return nil, fmt.Errorf("liste des serveurs : %w", err)
		}
		out = append(out, s)
	}
	return out, lignes.Err()
}

// ListerServeursSansAffectationActive renvoie les serveurs qui n'ont aucune
// affectation ouverte (date_fin IS NULL) dans le seau considéré, triés par date
// de fin de lease croissante (les serveurs sans date de lease en dernier).
// C'est la liste d'aide à la réutilisation du cadrage.
//
// Seau : scenarioID nil ne considère que le réel (serveurs et affectations
// scenario_id IS NULL). Une valeur applique la règle de surcharge du §6 de
// modele-donnees.md, comme AffectationsResolues : pour un serveur touché par
// le scénario (au moins une ligne d'affectation dans son seau), seul ce seau
// compte — un serveur retiré dans le scénario est donc candidat à la
// réutilisation sous ce scénario, un serveur déplacé ne l'est pas ; un
// serveur non touché suit le réel. Les serveurs hypothétiques du scénario
// sont inclus. La fin de lease provient de la révision courante du serveur
// (serveur_revision ouverte → revision → modele) : date_debut_lease du modèle
// décalée de duree_lease_mois.
func (d *Depot) ListerServeursSansAffectationActive(scenarioID *int64) ([]ServeurSansAffectation, error) {
	// un serveur décommissionné n'est pas un candidat à la réutilisation
	// (revue 2026-09-12) : il a quitté le parc, ses périodes sont closes.
	filtreServeur := "s.scenario_id IS NULL AND s.statut <> 'DECOMMISSIONNE'"
	filtreAffect := "a.scenario_id IS NULL"
	var args []any
	if scenarioID != nil {
		filtreServeur = "(s.scenario_id IS NULL OR s.scenario_id = ?) AND s.statut <> 'DECOMMISSIONNE'"
		// le seau qui gouverne ce serveur : celui du scénario s'il y a une
		// ligne, sinon le réel (-1, la convention COALESCE de la migration 0002)
		filtreAffect = `COALESCE(a.scenario_id, -1) = CASE
			WHEN EXISTS (SELECT 1 FROM affectation t WHERE t.serveur_id = s.id AND t.scenario_id = ?)
			THEN ? ELSE -1 END`
		args = append(args, *scenarioID, *scenarioID, *scenarioID)
	}

	requete := `
		SELECT ` + prefixer("s.", colonnesServeur) + `,
		       m.date_debut_lease, m.duree_lease_mois
		FROM serveur s
		LEFT JOIN serveur_revision sr ON sr.serveur_id = s.id AND sr.date_fin IS NULL
		LEFT JOIN revision rev        ON rev.id = sr.revision_id
		LEFT JOIN modele m            ON m.id = rev.modele_id
		WHERE ` + filtreServeur + `
		  AND NOT EXISTS (
		        SELECT 1 FROM affectation a
		        WHERE a.serveur_id = s.id AND a.date_fin IS NULL
		          AND ` + filtreAffect + `
		  )
		ORDER BY s.id`

	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("serveurs sans affectation active", err)
	}
	defer lignes.Close()

	var out []ServeurSansAffectation
	for lignes.Next() {
		var s Serveur
		var debutLease *string
		var dureeLease *int64
		cibles := append(ciblesServeur(&s), &debutLease, &dureeLease)
		if err := lignes.Scan(cibles...); err != nil {
			return nil, fmt.Errorf("serveurs sans affectation active : %w", err)
		}
		item := ServeurSansAffectation{Serveur: s}
		if debutLease != nil && dureeLease != nil {
			fin, err := ajouterMois(*debutLease, int(*dureeLease))
			if err != nil {
				return nil, fmt.Errorf("serveurs sans affectation active : %w", err)
			}
			item.FinLease = &fin
		}
		out = append(out, item)
	}
	if err := lignes.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].FinLease, out[j].FinLease
		switch {
		case a == nil && b == nil:
			return out[i].Serveur.ID < out[j].Serveur.ID
		case a == nil:
			return false
		case b == nil:
			return true
		case *a != *b:
			return *a < *b
		default:
			return out[i].Serveur.ID < out[j].Serveur.ID
		}
	})
	return out, nil
}

// NomsPhysiquesServeurs renvoie, pour les identifiants demandés, le nom
// physique de chaque serveur (chaîne vide si non renseigné) — utile pour
// enrichir d'un libellé une liste d'identifiants obtenue ailleurs (écran de
// delta d'un scénario) sans repasser par une lecture complète par serveur.
func (d *Depot) NomsPhysiquesServeurs(ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	marques := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marques[i] = "?"
		args[i] = id
	}
	lignes, err := d.base.Query(
		`SELECT id, COALESCE(physical_name, '') FROM serveur WHERE id IN (`+strings.Join(marques, ",")+`)`,
		args...)
	if err != nil {
		return nil, traduire("noms physiques des serveurs", err)
	}
	defer lignes.Close()

	for lignes.Next() {
		var id int64
		var nom string
		if err := lignes.Scan(&id, &nom); err != nil {
			return nil, fmt.Errorf("noms physiques des serveurs : %w", err)
		}
		out[id] = nom
	}
	return out, lignes.Err()
}

// ---------------------------------------------------------------- helpers

// prefixer préfixe chaque colonne d'une liste « a, b, c » par p (« s.a, s.b »).
func prefixer(p, colonnes string) string {
	parts := strings.Split(colonnes, ",")
	for i := range parts {
		parts[i] = p + strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ", ")
}

func lireServeur(c conn, id int64) (Serveur, error) {
	var s Serveur
	err := scanUn(
		c.QueryRow(`SELECT `+colonnesServeur+` FROM serveur WHERE id = ?`, id),
		fmt.Sprintf("lecture du serveur %d", id),
		ciblesServeur(&s)...)
	return s, err
}
