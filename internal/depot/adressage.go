package depot

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Adressage IP (backlog v3.1) : proposition d'adresses aux serveurs d'une
// hypothèse et signalement des incohérences.
//
// Le pool est global : une adresse portée par tout serveur non décommissionné
// — réel, ou hypothétique d'un scénario non abandonné — est prise, quel que
// soit le scénario. Deux hypothèses concurrentes ne peuvent donc jamais se
// voir proposer la même adresse. L'attribution est un geste explicite (« au
// moment de la demande de devis »), jamais un effet de la création d'un
// serveur.
//
// Le résultat n'est qu'une proposition : forcer une adresse, c'est la saisir
// dans le champ ip de la fiche serveur, dans la plage ou non — l'anomalie se
// verra ensuite. Rien ici n'est bloquant.

// Issues possibles pour un serveur lors d'une attribution en lot.
const (
	AdressageAttribue     = "ATTRIBUE"      // adresse posée sur le serveur
	AdressageDejaAdresse  = "DEJA_ADRESSE"  // le serveur portait déjà une adresse : non touché
	AdressageAChoisir     = "A_CHOISIR"     // plusieurs VLAN applicables : l'utilisateur choisit
	AdressageSansVlan     = "SANS_VLAN"     // aucun VLAN applicable (ou pas d'affectation)
	AdressagePlageEpuisee = "PLAGE_EPUISEE" // toutes les adresses des plages du VLAN sont prises
)

// LigneAdressage détaille le sort d'un serveur dans une attribution en lot.
type LigneAdressage struct {
	ServeurID    int64
	PhysicalName string
	ClusterNom   string // vide sans affectation active dans le scénario
	Issue        string // l'une des constantes Adressage*
	IP           string // ATTRIBUE et DEJA_ADRESSE
	VlanCode     string // ATTRIBUE, DEJA_ADRESSE (si connu) et PLAGE_EPUISEE
	Candidats    []Vlan // A_CHOISIR : du plus spécifique au moins spécifique
	Detail       string // phrase explicative, affichable telle quelle
}

// ResultatAdressage est le compte rendu d'AdresserScenario (ou de sa
// simulation), une ligne par serveur HYPOTHESE du scénario.
type ResultatAdressage struct {
	ScenarioID int64
	Lignes     []LigneAdressage
	Comptes    map[string]int // par issue
}

// Types d'anomalies réseau.
const (
	AnomalieAdresseMultiple  = "ADRESSE_MULTIPLE"  // même adresse sur plusieurs serveurs
	AnomalieHorsPlage        = "HORS_PLAGE"        // adresse hors des plages de son VLAN
	AnomalieVlanIncompatible = "VLAN_INCOMPATIBLE" // VLAN hors des critères du cluster / de la zone
	AnomalieSansVlan         = "SANS_VLAN"         // adresse renseignée, VLAN absent
	AnomalieIPInvalide       = "IP_INVALIDE"       // texte qui n'est pas une IPv4
)

// AnomalieReseau est une incohérence d'adressage constatée à la lecture.
// Jamais bloquante : l'import serveurs et la saisie manuelle passent,
// l'anomalie se voit ensuite.
type AnomalieReseau struct {
	ServeurID    int64
	PhysicalName string
	IP           string
	VlanCode     string
	Type         string
	Detail       string
}

// ---------------------------------------------------------------- pool

// AdressesPrises renvoie, par adresse (forme canonique), les serveurs qui la
// portent parmi ceux qui comptent dans le pool : non décommissionnés, réels
// ou d'un scénario non abandonné.
func (d *Depot) AdressesPrises() (map[string][]int64, error) {
	return adressesPrises(d.base)
}

func adressesPrises(c conn) (map[string][]int64, error) {
	lignes, err := c.Query(
		`SELECT s.id, s.ip FROM serveur s
		 LEFT JOIN scenario sc ON sc.id = s.scenario_id
		 WHERE s.ip IS NOT NULL AND TRIM(s.ip) <> ''
		   AND s.statut <> 'DECOMMISSIONNE'
		   AND (s.scenario_id IS NULL OR sc.statut <> 'ABANDONNE')
		 ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("adresses prises : %w", err)
	}
	defer lignes.Close()

	out := map[string][]int64{}
	for lignes.Next() {
		var id int64
		var ip string
		if err := lignes.Scan(&id, &ip); err != nil {
			return nil, fmt.Errorf("adresses prises : %w", err)
		}
		cle := NormaliserIPv4(ip)
		out[cle] = append(out[cle], id)
	}
	return out, lignes.Err()
}

// ---------------------------------------------------------------- proposition

// ProposerAdresse renvoie la première adresse libre après le curseur du VLAN
// (DerniereIP), dans ses plages prises dans l'ordre des adresses de début,
// en rebouclant au début de la première plage. Sans curseur — ou curseur
// invalide — on part du début. Faux si toutes les adresses sont prises ou
// si le VLAN n'a aucune plage valide.
//
// Repartir après le curseur plutôt que du début est ce qui fait qu'une
// adresse libérée n'est reprise qu'au tour suivant, le temps qu'un
// décommissionnement en cours se termine réellement.
func ProposerAdresse(v Vlan, prises map[string][]int64) (string, bool) {
	var intervalles []IntervalleIPv4
	for _, p := range v.Plages {
		it, err := p.Intervalle()
		if err != nil {
			continue // borne corrompue en base : on l'ignore plutôt que d'échouer
		}
		intervalles = append(intervalles, it)
	}
	if len(intervalles) == 0 {
		return "", false
	}
	sort.Slice(intervalles, func(i, j int) bool { return intervalles[i].Debut < intervalles[j].Debut })

	libre := func(ip uint32) bool {
		_, prise := prises[FormaterIPv4(ip)]
		return !prise
	}
	premiereLibre := func(it IntervalleIPv4) (uint32, bool) {
		var trouvee uint32
		ok := false
		it.Parcourir(func(ip uint32) bool {
			if libre(ip) {
				trouvee, ok = ip, true
				return false
			}
			return true
		})
		return trouvee, ok
	}

	var curseur uint32
	aCurseur := false
	if v.DerniereIP != nil {
		if c, err := ParserIPv4(*v.DerniereIP); err == nil {
			curseur, aCurseur = c, true
		}
	}

	// premier tour : strictement après le curseur
	if aCurseur {
		for _, it := range intervalles {
			if it.Fin <= curseur {
				continue
			}
			if it.Debut <= curseur {
				it.Debut = curseur + 1
			}
			if ip, ok := premiereLibre(it); ok {
				return FormaterIPv4(ip), true
			}
		}
	}
	// rebouclage (ou départ sans curseur) : du début jusqu'au curseur inclus
	for _, it := range intervalles {
		if aCurseur {
			if it.Debut > curseur {
				break
			}
			if it.Fin > curseur {
				it.Fin = curseur
			}
		}
		if ip, ok := premiereLibre(it); ok {
			return FormaterIPv4(ip), true
		}
	}
	return "", false
}

// ---------------------------------------------------------------- attribution

// AdresserScenario attribue, en une transaction, une adresse à chaque serveur
// HYPOTHESE du scénario qui n'en a pas encore, dans un ordre stable (cluster
// puis nom). Le VLAN est celui déjà posé sur le serveur, sinon déduit de
// son affectation active dans le seau du scénario (cluster -> projet,
// environnement) et de sa zone :
//
//   - exactement un VLAN applicable -> adresse posée (serveur.ip,
//     serveur.vlan_id, vlan.derniere_ip) ;
//   - plusieurs -> ligne A_CHOISIR avec les candidats, rien d'écrit pour ce
//     serveur : AdresserServeur avec le VLAN choisi ;
//   - aucun (ou pas d'affectation) -> SANS_VLAN ;
//   - toutes les adresses prises -> PLAGE_EPUISEE.
//
// Les serveurs qui portaient déjà une adresse sont listés DEJA_ADRESSE et
// jamais touchés. Deux appels successifs ne redonnent jamais la même
// adresse : le curseur avance et le pool est relu à chaque appel.
//
// Erreurs : ErrIntrouvable si le scénario n'existe pas ; ErrValidation s'il
// est abandonné (ses serveurs ne comptent plus dans le pool, les adresser
// n'aurait pas de sens).
func (d *Depot) AdresserScenario(scenarioID int64) (ResultatAdressage, error) {
	var res ResultatAdressage
	err := d.enTx(func(tx *sql.Tx) error {
		var err error
		res, err = d.adresserScenarioTx(tx, scenarioID)
		return err
	})
	return res, err
}

// SimulerAdressage calcule ce qu'AdresserScenario ferait, sans rien écrire :
// même code, dans une transaction toujours annulée. Sert à l'aperçu avant
// le geste, et à relire l'état d'adressage d'une hypothèse après coup.
func (d *Depot) SimulerAdressage(scenarioID int64) (ResultatAdressage, error) {
	tx, err := d.base.Begin()
	if err != nil {
		return ResultatAdressage{}, fmt.Errorf("ouverture de la transaction : %w", err)
	}
	defer tx.Rollback() //nolint:errcheck — l'annulation est le but
	return d.adresserScenarioTx(tx, scenarioID)
}

// serveurAAdresser est une ligne de la requête d'AdresserScenario : le
// serveur et, s'il en a une, son affectation active dans le seau du scénario.
type serveurAAdresser struct {
	ID           int64
	PhysicalName string
	IP           *string
	VlanID       *int64
	ZoneID       *int64
	ClusterID    *int64
	ClusterNom   *string
	ProjetID     *int64
	EnvID        *int64
}

func (d *Depot) adresserScenarioTx(tx *sql.Tx, scenarioID int64) (ResultatAdressage, error) {
	sc, err := lireScenario(tx, scenarioID)
	if err != nil {
		return ResultatAdressage{}, err
	}
	if sc.Statut == ScenarioAbandonne {
		return ResultatAdressage{}, fmt.Errorf("adressage du scénario %d : %w : scénario abandonné",
			scenarioID, ErrValidation)
	}

	lignes, err := tx.Query(
		`SELECT s.id, COALESCE(s.physical_name, ''), s.ip, s.vlan_id, s.zone_id,
		        c.id, c.nom, c.projet_id, c.environnement_id
		 FROM serveur s
		 LEFT JOIN affectation a ON a.serveur_id = s.id AND a.scenario_id = ? AND a.date_fin IS NULL
		 LEFT JOIN cluster c ON c.id = a.cluster_id
		 WHERE s.statut = 'HYPOTHESE' AND s.scenario_id = ?
		 ORDER BY c.nom, s.physical_name, s.id`, scenarioID, scenarioID)
	if err != nil {
		return ResultatAdressage{}, fmt.Errorf("serveurs à adresser du scénario %d : %w", scenarioID, err)
	}
	var serveurs []serveurAAdresser
	for lignes.Next() {
		var s serveurAAdresser
		if err := lignes.Scan(&s.ID, &s.PhysicalName, &s.IP, &s.VlanID, &s.ZoneID,
			&s.ClusterID, &s.ClusterNom, &s.ProjetID, &s.EnvID); err != nil {
			lignes.Close()
			return ResultatAdressage{}, fmt.Errorf("serveurs à adresser du scénario %d : %w", scenarioID, err)
		}
		serveurs = append(serveurs, s)
	}
	if err := lignes.Close(); err != nil {
		return ResultatAdressage{}, err
	}

	prises, err := adressesPrises(tx)
	if err != nil {
		return ResultatAdressage{}, err
	}
	// Les VLAN sont mis en cache pour que le curseur avancé par une
	// attribution serve à la suivante dans le même VLAN, sans relecture.
	vlans := map[int64]*Vlan{}
	cacher := func(v Vlan) *Vlan {
		if c, ok := vlans[v.ID]; ok {
			return c
		}
		copie := v
		vlans[v.ID] = &copie
		return &copie
	}

	res := ResultatAdressage{ScenarioID: scenarioID, Comptes: map[string]int{}}
	for _, s := range serveurs {
		l := LigneAdressage{ServeurID: s.ID, PhysicalName: s.PhysicalName}
		if s.ClusterNom != nil {
			l.ClusterNom = *s.ClusterNom
		}

		if s.IP != nil && strings.TrimSpace(*s.IP) != "" {
			l.Issue, l.IP = AdressageDejaAdresse, strings.TrimSpace(*s.IP)
			if s.VlanID != nil {
				if v, err := lireVlan(tx, *s.VlanID); err == nil {
					l.VlanCode = v.Code
				}
			}
			l.Detail = "adresse déjà renseignée, serveur non touché"
			res.ajouter(l)
			continue
		}

		var vlan *Vlan
		switch {
		case s.VlanID != nil:
			v, err := lireVlan(tx, *s.VlanID)
			if err != nil {
				return ResultatAdressage{}, err
			}
			vlan = cacher(v)
		case s.ClusterID == nil:
			l.Issue = AdressageSansVlan
			l.Detail = "aucune affectation active dans ce scénario : impossible de déduire un VLAN"
			res.ajouter(l)
			continue
		default:
			candidats, err := vlansApplicables(tx, *s.ProjetID, *s.EnvID, s.ZoneID, *s.ClusterID)
			if err != nil {
				return ResultatAdressage{}, err
			}
			switch len(candidats) {
			case 0:
				l.Issue = AdressageSansVlan
				l.Detail = "aucun VLAN applicable au cluster " + l.ClusterNom + " et à la zone du serveur"
				res.ajouter(l)
				continue
			case 1:
				vlan = cacher(candidats[0])
			default:
				l.Issue = AdressageAChoisir
				for _, c := range candidats {
					l.Candidats = append(l.Candidats, *cacher(c))
				}
				l.Detail = fmt.Sprintf("%d VLAN applicables : %s", len(candidats), codesVlans(l.Candidats))
				res.ajouter(l)
				continue
			}
		}

		l.VlanCode = vlan.Code
		ip, ok := ProposerAdresse(*vlan, prises)
		if !ok {
			l.Issue = AdressagePlageEpuisee
			l.Detail = "aucune adresse libre dans les plages du VLAN " + vlan.Code
			res.ajouter(l)
			continue
		}
		if err := d.attribuerTx(tx, s.ID, vlan, ip); err != nil {
			return ResultatAdressage{}, err
		}
		prises[ip] = append(prises[ip], s.ID)
		l.Issue, l.IP = AdressageAttribue, ip
		l.Detail = "adresse proposée dans le VLAN " + vlan.Code
		res.ajouter(l)
	}
	return res, nil
}

func (r *ResultatAdressage) ajouter(l LigneAdressage) {
	r.Lignes = append(r.Lignes, l)
	r.Comptes[l.Issue]++
}

func codesVlans(vs []Vlan) string {
	codes := make([]string, len(vs))
	for i, v := range vs {
		codes[i] = v.Code
	}
	return strings.Join(codes, ", ")
}

// attribuerTx pose l'adresse et le VLAN sur le serveur et avance le curseur
// du VLAN — en base et sur l'objet en cache, pour la proposition suivante.
func (d *Depot) attribuerTx(tx *sql.Tx, serveurID int64, vlan *Vlan, ip string) error {
	avant, err := instantane(tx, "serveur", serveurID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE serveur SET ip = ?, vlan_id = ? WHERE id = ?`, ip, vlan.ID, serveurID); err != nil {
		return traduire(fmt.Sprintf("attribution de %s au serveur %d", ip, serveurID), err)
	}
	if _, err := tx.Exec(`UPDATE vlan SET derniere_ip = ? WHERE id = ?`, ip, vlan.ID); err != nil {
		return traduire(fmt.Sprintf("curseur du VLAN %s", vlan.Code), err)
	}
	curseur := ip
	vlan.DerniereIP = &curseur
	// le curseur du VLAN est un état technique, pas une décision : seule
	// l'attribution au serveur est journalisée.
	return d.journalModificationTable(tx, "serveur", "serveur", serveurID, ActionModification, avant)
}

// AdresserServeur propose et pose une adresse à un seul serveur dans le VLAN
// choisi — le geste qui suit une ligne A_CHOISIR, ou l'adressage d'un
// serveur isolé. Même règle de proposition que l'attribution en lot.
//
// Erreurs : ErrIntrouvable si le serveur ou le VLAN n'existe pas ;
// ErrValidation si le serveur porte déjà une adresse (la forcer se fait en
// la saisissant), s'il est décommissionné, ou si les plages du VLAN sont
// épuisées.
func (d *Depot) AdresserServeur(serveurID, vlanID int64) (string, error) {
	var ip string
	err := d.enTx(func(tx *sql.Tx) error {
		s, err := lireServeur(tx, serveurID)
		if err != nil {
			return err
		}
		if s.Statut == StatutDecommissionne {
			return fmt.Errorf("adressage du serveur %d : %w : serveur décommissionné", serveurID, ErrValidation)
		}
		if s.IP != nil && strings.TrimSpace(*s.IP) != "" {
			return fmt.Errorf("adressage du serveur %d : %w : il porte déjà l'adresse %s (la vider ou la modifier sur la fiche)",
				serveurID, ErrValidation, strings.TrimSpace(*s.IP))
		}
		vlan, err := lireVlan(tx, vlanID)
		if err != nil {
			return err
		}
		prises, err := adressesPrises(tx)
		if err != nil {
			return err
		}
		proposee, ok := ProposerAdresse(vlan, prises)
		if !ok {
			return fmt.Errorf("adressage du serveur %d : %w : aucune adresse libre dans les plages du VLAN %s",
				serveurID, ErrValidation, vlan.Code)
		}
		if err := d.attribuerTx(tx, serveurID, &vlan, proposee); err != nil {
			return err
		}
		ip = proposee
		return nil
	})
	return ip, err
}

// ---------------------------------------------------------------- anomalies

// serveurReseau est une ligne de la requête d'AnomaliesReseau : le serveur,
// son VLAN éventuel, et le cluster de son affectation active — dans le seau
// de son scénario pour un serveur hypothétique, dans le réel sinon.
type serveurReseau struct {
	ID           int64
	PhysicalName string
	IP           *string
	VlanID       *int64
	ZoneID       *int64
	ClusterID    *int64
	ProjetID     *int64
	EnvID        *int64
}

// AnomaliesReseau calcule, à la lecture, les incohérences d'adressage des
// serveurs qui comptent dans le pool (non décommissionnés, réels ou d'un
// scénario non abandonné) : adresse multiple (une anomalie par serveur,
// nommant les autres), hors plage, VLAN incompatible avec le cluster de
// l'affectation active ou la zone, adresse sans VLAN, adresse invalide.
// Triées par nom de serveur puis par type.
func (d *Depot) AnomaliesReseau() ([]AnomalieReseau, error) {
	lignes, err := d.base.Query(
		`SELECT s.id, COALESCE(s.physical_name, ''), s.ip, s.vlan_id, s.zone_id,
		        c.id, c.projet_id, c.environnement_id
		 FROM serveur s
		 LEFT JOIN scenario sc ON sc.id = s.scenario_id
		 LEFT JOIN affectation a ON a.serveur_id = s.id AND a.date_fin IS NULL
		      AND COALESCE(a.scenario_id, -1) = COALESCE(s.scenario_id, -1)
		 LEFT JOIN cluster c ON c.id = a.cluster_id
		 WHERE s.statut <> 'DECOMMISSIONNE'
		   AND (s.scenario_id IS NULL OR sc.statut <> 'ABANDONNE')
		   AND ((s.ip IS NOT NULL AND TRIM(s.ip) <> '') OR s.vlan_id IS NOT NULL)
		 ORDER BY s.physical_name, s.id`)
	if err != nil {
		return nil, fmt.Errorf("anomalies réseau : %w", err)
	}
	var serveurs []serveurReseau
	for lignes.Next() {
		var s serveurReseau
		if err := lignes.Scan(&s.ID, &s.PhysicalName, &s.IP, &s.VlanID, &s.ZoneID,
			&s.ClusterID, &s.ProjetID, &s.EnvID); err != nil {
			lignes.Close()
			return nil, fmt.Errorf("anomalies réseau : %w", err)
		}
		serveurs = append(serveurs, s)
	}
	if err := lignes.Close(); err != nil {
		return nil, err
	}

	vlans, err := d.ListerVlans()
	if err != nil {
		return nil, err
	}
	vlanParID := make(map[int64]Vlan, len(vlans))
	for _, v := range vlans {
		vlanParID[v.ID] = v
	}

	// adresses partagées : regroupement par forme canonique
	porteurs := map[string][]serveurReseau{}
	for _, s := range serveurs {
		if s.IP == nil || strings.TrimSpace(*s.IP) == "" {
			continue
		}
		if _, err := ParserIPv4(*s.IP); err != nil {
			continue // signalée IP_INVALIDE, pas comparable aux autres
		}
		cle := NormaliserIPv4(*s.IP)
		porteurs[cle] = append(porteurs[cle], s)
	}

	var out []AnomalieReseau
	for _, s := range serveurs {
		base := AnomalieReseau{ServeurID: s.ID, PhysicalName: s.PhysicalName}
		if s.IP != nil {
			base.IP = strings.TrimSpace(*s.IP)
		}
		var vlan *Vlan
		if s.VlanID != nil {
			if v, ok := vlanParID[*s.VlanID]; ok {
				vlan = &v
				base.VlanCode = v.Code
			}
		}
		signaler := func(typ, detail string) {
			a := base
			a.Type, a.Detail = typ, detail
			out = append(out, a)
		}

		var ip uint32
		ipValide := false
		if base.IP != "" {
			v, err := ParserIPv4(base.IP)
			if err != nil {
				signaler(AnomalieIPInvalide, fmt.Sprintf("« %s » n'est pas une adresse IPv4", base.IP))
			} else {
				ip, ipValide = v, true
			}
		}

		if ipValide {
			autres := porteurs[FormaterIPv4(ip)]
			if len(autres) > 1 {
				var noms []string
				for _, a := range autres {
					if a.ID != s.ID {
						noms = append(noms, nomOuID(a.PhysicalName, a.ID))
					}
				}
				signaler(AnomalieAdresseMultiple, "adresse aussi portée par "+strings.Join(noms, ", "))
			}
			if vlan == nil {
				signaler(AnomalieSansVlan, "adresse renseignée sans VLAN")
			} else if len(vlan.Plages) > 0 {
				dedans := false
				for _, p := range vlan.Plages {
					if it, err := p.Intervalle(); err == nil && it.Contient(ip) {
						dedans = true
						break
					}
				}
				if !dedans {
					signaler(AnomalieHorsPlage, "adresse hors des plages du VLAN "+vlan.Code+" ("+plagesTexte(vlan.Plages)+")")
				}
			}
		}

		if vlan != nil {
			incompatible := false
			var raison string
			switch {
			case s.ClusterID != nil && !vlan.Applicable(*s.ProjetID, *s.EnvID, s.ZoneID, *s.ClusterID):
				incompatible = true
				raison = "le VLAN " + vlan.Code + " ne s'applique pas au cluster de l'affectation active ni à la zone du serveur"
			case s.ClusterID == nil && vlan.ZoneID != nil && (s.ZoneID == nil || *s.ZoneID != *vlan.ZoneID):
				incompatible = true
				raison = "le VLAN " + vlan.Code + " est réservé à une autre zone que celle du serveur"
			}
			if incompatible {
				signaler(AnomalieVlanIncompatible, raison)
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PhysicalName != out[j].PhysicalName {
			return out[i].PhysicalName < out[j].PhysicalName
		}
		if out[i].ServeurID != out[j].ServeurID {
			return out[i].ServeurID < out[j].ServeurID
		}
		return out[i].Type < out[j].Type
	})
	return out, nil
}

func nomOuID(nom string, id int64) string {
	if nom != "" {
		return nom
	}
	return fmt.Sprintf("serveur #%d", id)
}

// plagesTexte rend « 10.0.0.10–10.0.0.20, 10.0.1.0–10.0.1.50 ».
func plagesTexte(plages []PlageIP) string {
	parts := make([]string, len(plages))
	for i, p := range plages {
		parts[i] = p.IPDebut + "–" + p.IPFin
	}
	return strings.Join(parts, ", ")
}

// PlagesTexte est la forme affichable des plages d'un VLAN, pour les écrans
// et les exports.
func (v Vlan) PlagesTexte() string { return plagesTexte(v.Plages) }
