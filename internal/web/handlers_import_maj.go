package web

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/importation"
)

// Mise à jour en masse des serveurs (backlog v3.5) — l'import incrémental.
// Là où les autres imports créent, celui-ci modifie des serveurs réels
// existants depuis un fichier « adaptatif » : seules les colonnes présentes
// sont touchées, une cellule vide ne change rien, la sentinelle #VIDE
// efface. L'analyse produit un diff par ligne (champ : avant → après),
// rien n'est écrit sans confirmation, et l'écriture est une transaction
// unique (depot.AppliquerMajServeurs) — jamais d'import partiel. L'export
// « pour mise à jour » de la liste des serveurs produit le format exact
// attendu, pour l'aller-retour Excel.
func (s *serveur) routesImportMaj() {
	s.mux.HandleFunc("GET /import/serveurs/maj", s.lecteur(s.importMajPage))
	s.mux.HandleFunc("POST /import/serveurs/maj/analyser", s.editeur(s.importMajAnalyser))
	s.mux.HandleFunc("POST /import/serveurs/maj/confirmer", s.editeur(s.importMajConfirmer))
	s.mux.HandleFunc("GET /serveurs/export-maj", s.lecteur(s.serveursExportMaj))
}

// Sentinelle d'effacement : une cellule qui la porte met le champ à NULL.
const sentinelleVide = "#VIDE"

// colonnesMaj est le format complet, dans l'ordre de l'export ; le fichier
// importé peut n'en porter qu'un sous-ensemble.
var colonnesMaj = []string{
	"physical_name", "hostname", "serial_number", "zone_code", "position_zone",
	"ip", "vlan_code", "version_os", "typologie", "code_appli",
	"demande_ref", "demande_serveur_ref", "commentaire", "statut",
	"projet_code", "environnement_code", "cluster_nom", "date_affectation",
	"modele_code", "date_rattachement",
}

func (s *serveur) importMajPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import_maj"), "import_maj_page", map[string]any{
		"Colonnes": colonnesMaj, "Sentinelle": sentinelleVide,
	})
}

// ---------------------------------------------------------------- analyse

// ligneMaj est le résultat d'analyse d'une ligne : le serveur visé, la
// liste lisible des changements et le plan à appliquer.
type ligneMaj struct {
	NumeroLigne int
	Serveur     string   // libellé pour le rapport (nom physique ou hôte)
	Changements []string // « champ : avant → après »
	Plan        depot.MajServeur
}

// rapportMaj est le contexte du gabarit "import_maj_resultat".
type rapportMaj struct {
	MessageJeton     string
	NbLignes         int
	Erreurs          []importation.ErreurLigne
	Jeton            string
	Lignes           []ligneMaj // lignes avec au moins un changement
	NbSansChangement int
	Ecrit            bool
	NbModifies       int
}

// cellule lit une colonne : (valeur, présente dans l'en-tête et non vide).
func cellule(entetes map[string]bool, ligne map[string]string, col string) (string, bool) {
	if !entetes[col] {
		return "", false
	}
	v := strings.TrimSpace(ligne[col])
	return v, v != ""
}

// resoudreCible applique l'ordre des clés : hostname, puis demande + fiche,
// puis nom physique — la première qui désigne exactement un serveur réel
// l'emporte ; une clé qui en désigne plusieurs est une ambiguïté refusée.
// Le hostname sert à la fois de clé et de valeur : un fichier de réception
// identifie par demande + fiche et pose le hostname, qui ne matche encore
// rien — d'où l'ordre « première clé qui résout », pas « première clé
// présente ».
func (s *serveur) resoudreCible(entetes map[string]bool, ligne map[string]string) (int64, string, error) {
	type essai struct {
		libelle string
		ids     []int64
	}
	var essais []essai
	if h, ok := cellule(entetes, ligne, "hostname"); ok {
		ids, err := s.depot.ServeursParCle("hostname", h)
		if err != nil {
			return 0, "", err
		}
		essais = append(essais, essai{"hostname « " + h + " »", ids})
	}
	d, okD := cellule(entetes, ligne, "demande_ref")
	f, okF := cellule(entetes, ligne, "demande_serveur_ref")
	if okD && okF {
		ids, err := s.depot.ServeursParDemande(d, f)
		if err != nil {
			return 0, "", err
		}
		essais = append(essais, essai{"demande « " + d + " » fiche « " + f + " »", ids})
	}
	if p, ok := cellule(entetes, ligne, "physical_name"); ok {
		ids, err := s.depot.ServeursParCle("physical_name", p)
		if err != nil {
			return 0, "", err
		}
		essais = append(essais, essai{"nom physique « " + p + " »", ids})
	}
	if len(essais) == 0 {
		return 0, "", erreurLigneMetier{"aucune clé de rapprochement : hostname, demande_ref + demande_serveur_ref, ou physical_name"}
	}
	for _, e := range essais {
		switch len(e.ids) {
		case 1:
			return e.ids[0], e.libelle, nil
		case 0:
			continue
		default:
			return 0, "", erreurLigneMetier{fmt.Sprintf("%s désigne %d serveurs : ambigu", e.libelle, len(e.ids))}
		}
	}
	var libelles []string
	for _, e := range essais {
		libelles = append(libelles, e.libelle)
	}
	return 0, "", erreurLigneMetier{"aucun serveur réel ne correspond à " + strings.Join(libelles, ", ")}
}

// valeurTexte interprète une cellule d'attribut texte : sentinelle → nil
// (effacer), sinon la valeur. L'appelant a déjà écarté la cellule vide.
func valeurTexte(v string) *string {
	if v == sentinelleVide {
		return nil
	}
	return &v
}

func texteAffiche(p *string) string {
	if p == nil || *p == "" {
		return "—"
	}
	return *p
}

func memeTexte(a, b *string) bool {
	if a == nil || *a == "" {
		return b == nil || *b == ""
	}
	return b != nil && *a == *b
}

// analyserLigneMaj construit le plan d'une ligne. Les anomalies de saisie
// sont des erreurLigneMetier ; toute autre erreur est une panne.
func (s *serveur) analyserLigneMaj(entetes map[string]bool, ligne map[string]string, caches *cachesImportServeurs) (ligneMaj, error) {
	id, libelle, err := s.resoudreCible(entetes, ligne)
	if err != nil {
		return ligneMaj{}, err
	}
	actuel, err := s.depot.LireServeur(id)
	if err != nil {
		return ligneMaj{}, err
	}
	out := ligneMaj{Serveur: libelle, Plan: depot.MajServeur{ServeurID: id}}
	maj := actuel
	modifie := false
	var messages []string

	// ---- attributs texte
	champs := []struct {
		col   string
		cible **string
	}{
		{"physical_name", &maj.PhysicalName}, {"hostname", &maj.Hostname},
		{"serial_number", &maj.SerialNumber}, {"position_zone", &maj.PositionZone},
		{"ip", &maj.IP}, {"version_os", &maj.VersionOS}, {"code_appli", &maj.CodeAppli},
		{"demande_ref", &maj.DemandeRef}, {"demande_serveur_ref", &maj.DemandeServeurRef},
		{"commentaire", &maj.Commentaire},
	}
	for _, c := range champs {
		v, ok := cellule(entetes, ligne, c.col)
		if !ok {
			continue
		}
		nouvelle := valeurTexte(v)
		if memeTexte(*c.cible, nouvelle) {
			continue
		}
		out.Changements = append(out.Changements, fmt.Sprintf("%s : %s → %s", c.col, texteAffiche(*c.cible), texteAffiche(nouvelle)))
		*c.cible = nouvelle
		modifie = true
	}
	if v, ok := cellule(entetes, ligne, "typologie"); ok {
		nouvelle := valeurTexte(strings.ToUpper(v))
		if !depot.TypologieValide(nouvelle) {
			messages = append(messages, fmt.Sprintf("typologie « %s » inconnue (attendu : %s)", v, strings.Join(depot.Typologies, ", ")))
		} else if !memeTexte(maj.Typologie, nouvelle) {
			out.Changements = append(out.Changements, fmt.Sprintf("typologie : %s → %s", texteAffiche(maj.Typologie), texteAffiche(nouvelle)))
			maj.Typologie = nouvelle
			modifie = true
		}
	}
	if v, ok := cellule(entetes, ligne, "zone_code"); ok {
		var nouvelle *int64
		if v != sentinelleVide {
			nouvelle, err = s.resoudreZoneImport(v, caches.zones)
			if err != nil {
				if msg, ok := messageLigneMetier(err); ok {
					messages = append(messages, msg)
				} else {
					return ligneMaj{}, err
				}
			}
		}
		if !memeEntier(maj.ZoneID, nouvelle) {
			out.Changements = append(out.Changements, fmt.Sprintf("zone : %s → %s", s.libelleZone(maj.ZoneID), s.libelleZone(nouvelle)))
			maj.ZoneID = nouvelle
			modifie = true
		}
	}
	if v, ok := cellule(entetes, ligne, "vlan_code"); ok {
		var nouvelle *int64
		if v != sentinelleVide {
			vlan, err := s.depot.LireVlanParCode(v)
			if err != nil {
				if erreurMetier(err) {
					messages = append(messages, fmt.Sprintf("VLAN « %s » introuvable", v))
				} else {
					return ligneMaj{}, err
				}
			} else {
				nouvelle = &vlan.ID
			}
		}
		if !memeEntier(maj.VlanID, nouvelle) {
			out.Changements = append(out.Changements, fmt.Sprintf("vlan : %s → %s", s.libelleVlan(maj.VlanID), s.libelleVlan(nouvelle)))
			maj.VlanID = nouvelle
			modifie = true
		}
	}
	if modifie {
		out.Plan.Attributs = &maj
	}

	// ---- statut
	if v, ok := cellule(entetes, ligne, "statut"); ok {
		v = strings.ToUpper(v)
		switch v {
		case depot.StatutHypothese:
			messages = append(messages, "statut HYPOTHESE : un serveur hypothétique se crée depuis un scénario, pas par mise à jour")
		case depot.StatutCommande, depot.StatutEnService, depot.StatutDecommissionne:
			if v != actuel.Statut {
				out.Changements = append(out.Changements, fmt.Sprintf("statut : %s → %s", actuel.Statut, v))
				statut := v
				out.Plan.Statut = &statut
			}
		default:
			messages = append(messages, fmt.Sprintf("statut « %s » inconnu", v))
		}
	}

	// ---- rattachement de révision
	if v, ok := cellule(entetes, ligne, "modele_code"); ok {
		revID, err := s.resoudreRevisionRecenteImport(v, caches.revisions)
		if err != nil {
			if msg, ok := messageLigneMetier(err); ok {
				messages = append(messages, msg)
			} else {
				return ligneMaj{}, err
			}
		} else if revID != nil {
			ratts, err := s.depot.ListerRattachements(id)
			if err != nil {
				return ligneMaj{}, err
			}
			var ouvert *depot.Rattachement
			for i := range ratts {
				if ratts[i].DateFin == nil {
					ouvert = &ratts[i]
				}
			}
			if ouvert == nil || ouvert.RevisionID != *revID {
				date, okDate := cellule(entetes, ligne, "date_rattachement")
				switch {
				case !okDate:
					messages = append(messages, "changement de modèle : date_rattachement obligatoire")
				case depot.ValiderDate("date_rattachement", date) != nil:
					messages = append(messages, "date_rattachement « "+date+" » n'est pas au format YYYY-MM-DD")
				case ouvert != nil && date <= ouvert.DateDebut:
					messages = append(messages, fmt.Sprintf("date_rattachement %s doit être postérieure au début du rattachement courant (%s)", date, ouvert.DateDebut))
				default:
					avant := "aucune révision"
					if ouvert != nil {
						avant = s.libelleRevision(ouvert.RevisionID)
					}
					out.Changements = append(out.Changements, fmt.Sprintf("modèle : %s → %s à partir du %s", avant, s.libelleRevision(*revID), date))
					out.Plan.Rattachement = &depot.MajRattachement{RevisionID: *revID, Date: date}
				}
			}
		}
	}

	// ---- affectation (les trois codes ensemble, règle de l'import de création)
	pc, _ := cellule(entetes, ligne, "projet_code")
	ec, _ := cellule(entetes, ligne, "environnement_code")
	cn, _ := cellule(entetes, ligne, "cluster_nom")
	if pc != "" || ec != "" || cn != "" {
		clusterID, err := s.resoudreClusterImport(pc, ec, cn, caches.clusters)
		if err != nil {
			if msg, ok := messageLigneMetier(err); ok {
				messages = append(messages, msg)
			} else {
				return ligneMaj{}, err
			}
		} else if clusterID != nil {
			vies, err := s.depot.ListerAffectationsServeur(id)
			if err != nil {
				return ligneMaj{}, err
			}
			var active *depot.Affectation
			for i := range vies {
				if vies[i].ScenarioID == nil && vies[i].DateFin == nil {
					active = &vies[i]
				}
			}
			if active == nil || active.ClusterID != *clusterID {
				date, okDate := cellule(entetes, ligne, "date_affectation")
				switch {
				case !okDate:
					messages = append(messages, "changement d'affectation : date_affectation obligatoire")
				case depot.ValiderDate("date_affectation", date) != nil:
					messages = append(messages, "date_affectation « "+date+" » n'est pas au format YYYY-MM-DD")
				case active != nil && date <= active.DateDebut:
					messages = append(messages, fmt.Sprintf("date_affectation %s doit être postérieure au début de l'affectation courante (%s)", date, active.DateDebut))
				default:
					avant := "sans affectation"
					if active != nil {
						avant = s.libelleCluster(active.ClusterID)
					}
					out.Changements = append(out.Changements, fmt.Sprintf("affectation : %s → %s à partir du %s", avant, cn, date))
					out.Plan.Affectation = &depot.MajAffectation{ClusterID: *clusterID, Date: date, Reaffecter: active != nil}
				}
			}
		}
	}

	if len(messages) > 0 {
		return ligneMaj{}, erreurLigneMetier{strings.Join(messages, " ; ")}
	}
	return out, nil
}

func memeEntier(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s *serveur) libelleZone(id *int64) string {
	if id == nil {
		return "—"
	}
	if z, err := s.depot.LireZone(*id); err == nil {
		return z.Code
	}
	return fmt.Sprintf("#%d", *id)
}

func (s *serveur) libelleVlan(id *int64) string {
	if id == nil {
		return "—"
	}
	if v, err := s.depot.LireVlan(*id); err == nil {
		return v.Code
	}
	return fmt.Sprintf("#%d", *id)
}

func (s *serveur) libelleCluster(id int64) string {
	if c, err := s.depot.LireCluster(id); err == nil {
		return c.Nom
	}
	return fmt.Sprintf("#%d", id)
}

func (s *serveur) libelleRevision(id int64) string {
	rev, err := s.depot.LireRevision(id)
	if err != nil {
		return fmt.Sprintf("#%d", id)
	}
	if m, err := s.depot.LireModele(rev.ModeleID); err == nil {
		return fmt.Sprintf("%s rév. %d", m.Code, rev.Numero)
	}
	return fmt.Sprintf("révision %d", rev.Numero)
}

// analyserFichierMaj décode et analyse tout le fichier, sans écrire.
func (s *serveur) analyserFichierMaj(contenu []byte) (importation.Rapport, []ligneMaj, int, error) {
	entetes, lignes, numeros, err := importation.LireCSV(bytes.NewReader(contenu))
	if err != nil {
		return importation.Rapport{Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: err.Error()}}}, nil, 0, nil
	}
	presentes := map[string]bool{}
	connue := map[string]bool{}
	for _, c := range colonnesMaj {
		connue[c] = true
	}
	for _, e := range entetes {
		if connue[e] {
			presentes[e] = true
		}
	}
	if len(presentes) == 0 {
		return importation.Rapport{Erreurs: []importation.ErreurLigne{{
			Ligne: 1, Message: "aucune colonne connue dans l'en-tête (attendu parmi : " + strings.Join(colonnesMaj, ", ") + ")",
		}}}, nil, 0, nil
	}

	caches := nouvellesCachesImportServeurs()
	var erreurs []importation.ErreurLigne
	var plans []ligneMaj
	sansChangement := 0
	vus := map[int64]int{}
	for i, ligne := range lignes {
		// le doublon se détecte sur la cible résolue, que la première ligne
		// soit valide ou non : deux lignes pour un même serveur, c'est une
		// intention contradictoire dans les deux cas.
		if id, _, errCible := s.resoudreCible(presentes, ligne); errCible == nil {
			if premiere, deja := vus[id]; deja {
				erreurs = append(erreurs, importation.ErreurLigne{Ligne: numeros[i], Message: fmt.Sprintf("serveur déjà visé par la ligne %d", premiere)})
				continue
			}
			vus[id] = numeros[i]
		}
		l, err := s.analyserLigneMaj(presentes, ligne, caches)
		if err != nil {
			if msg, ok := messageLigneMetier(err); ok {
				erreurs = append(erreurs, importation.ErreurLigne{Ligne: numeros[i], Message: msg})
				continue
			}
			return importation.Rapport{}, nil, 0, err
		}
		l.NumeroLigne = numeros[i]
		if l.Plan.Vide() {
			sansChangement++
			continue
		}
		plans = append(plans, l)
	}
	return importation.Rapport{NbLignes: len(lignes), Erreurs: erreurs}, plans, sansChangement, nil
}

// ---------------------------------------------------------------- écran

func (s *serveur) importMajAnalyser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	fichier, _, err := r.FormFile("fichier")
	if err != nil {
		s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{
			Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: "aucun fichier reçu"}},
		})
		return
	}
	defer fichier.Close()
	contenu, err := io.ReadAll(fichier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	rapport, plans, sans, err := s.analyserFichierMaj(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs})
		return
	}
	jeton, err := importsEnAttente.Deposer(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{
		NbLignes: rapport.NbLignes, Jeton: jeton, Lignes: plans, NbSansChangement: sans,
	})
}

func (s *serveur) importMajConfirmer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	jeton := r.FormValue("jeton")
	contenu, ok := importsEnAttente.Recuperer(jeton)
	if !ok {
		s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{
			MessageJeton: "Cette mise à jour a expiré ou a déjà été appliquée — réanalysez le fichier.",
		})
		return
	}
	defer importsEnAttente.Consommer(jeton)

	// revalidation complète : la base a pu changer depuis l'analyse.
	rapport, plans, sans, err := s.analyserFichierMaj(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs})
		return
	}
	majs := make([]depot.MajServeur, len(plans))
	for i, p := range plans {
		majs[i] = p.Plan
	}
	n, err := s.depotPour(r).AppliquerMajServeurs(majs)
	if err != nil {
		if erreurMetier(err) {
			// découvert à l'écriture (chevauchement…) : rien n'a été écrit.
			s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{
				NbLignes: rapport.NbLignes,
				Erreurs:  []importation.ErreurLigne{{Ligne: 0, Message: "refusé à l'écriture, rien n'a été modifié : " + messageUtilisateur(err)}},
			})
			return
		}
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_maj_resultat", rapportMaj{
		NbLignes: rapport.NbLignes, Ecrit: true, NbModifies: n, NbSansChangement: sans,
	})
}

// ---------------------------------------------------------------- export aller-retour

// serveursExportMaj produit le CSV au format exact de l'import, pour les
// serveurs réels retenus par les filtres de la liste : on exporte, on
// modifie dans Excel, on réimporte. Les colonnes de date restent vides :
// elles ne se renseignent que pour un changement d'affectation ou de
// modèle.
func (s *serveur) serveursExportMaj(w http.ResponseWriter, r *http.Request) {
	f, err := serveurFiltreDepuisRequete(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	conditions := []string{"s.scenario_id IS NULL"}
	var args []any
	if f.Statut != nil {
		conditions = append(conditions, "s.statut = ?")
		args = append(args, *f.Statut)
	}
	if f.ZoneID != nil {
		conditions = append(conditions, "s.zone_id = ?")
		args = append(args, *f.ZoneID)
	}
	lignes, err := s.depot.Base().Query(`
		SELECT COALESCE(s.physical_name, ''), COALESCE(s.hostname, ''), COALESCE(s.serial_number, ''),
		       COALESCE(z.code, ''), COALESCE(s.position_zone, ''), COALESCE(s.ip, ''), COALESCE(v.code, ''),
		       COALESCE(s.version_os, ''), COALESCE(s.typologie, ''), COALESCE(s.code_appli, ''),
		       COALESCE(s.demande_ref, ''), COALESCE(s.demande_serveur_ref, ''), COALESCE(s.commentaire, ''),
		       s.statut, COALESCE(pr.code, ''), COALESCE(env.code, ''), COALESCE(c.nom, ''), COALESCE(m.code, '')
		FROM serveur s
		LEFT JOIN zone z ON z.id = s.zone_id
		LEFT JOIN vlan v ON v.id = s.vlan_id
		LEFT JOIN affectation a ON a.serveur_id = s.id AND a.scenario_id IS NULL AND a.date_fin IS NULL
		LEFT JOIN cluster c ON c.id = a.cluster_id
		LEFT JOIN projet pr ON pr.id = c.projet_id
		LEFT JOIN environnement env ON env.id = c.environnement_id
		LEFT JOIN serveur_revision sr ON sr.serveur_id = s.id AND sr.date_fin IS NULL
		LEFT JOIN revision rev ON rev.id = sr.revision_id
		LEFT JOIN modele m ON m.id = rev.modele_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY s.physical_name, s.id`, args...)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	defer lignes.Close()

	t := tableau{Titre: "serveurs mise à jour", Colonnes: colonnesMaj}
	for lignes.Next() {
		v := make([]string, 18)
		cibles := make([]any, len(v))
		for i := range v {
			cibles[i] = &v[i]
		}
		if err := lignes.Scan(cibles...); err != nil {
			s.erreurServeur(w, r, err)
			return
		}
		// colonnes dans l'ordre de colonnesMaj ; les deux dates restent vides.
		t.Lignes = append(t.Lignes, []any{
			v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], v[11], v[12], v[13],
			v[14], v[15], v[16], "", v[17], "",
		})
	}
	if err := lignes.Err(); err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	ecrireTableauCSV(w, t)
}
