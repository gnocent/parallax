package web

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/importation"
)

// handlers_import_serveurs.go porte la reprise des serveurs (v1.4 du
// backlog) : import CSV avec résolution des références (zone, cluster,
// modèle), rapport d'erreurs ligne à ligne, et mode simulation avant
// validation. Critère d'acceptation du backlog : « aucun import partiel
// n'est laissé en base » — d'où l'écriture dans une seule transaction
// (s.depot.Base().Begin()), après une validation complète de toutes les
// lignes, jamais l'inverse.
//
// Les méthodes d'écriture du dépôt (CreerServeur, RattacherRevision,
// Affecter) ouvrent chacune leur propre transaction sur d.base : aucune ne
// permet à l'appelant de partager une transaction unique sur ~1800 lignes.
// C'est pourquoi ce fichier écrit directement en SQL, dans une transaction
// dédiée, tout en réutilisant les méthodes de lecture du dépôt pour
// résoudre les références avant d'écrire.
//
// Lignes à ajouter dans server.go (routes()), à côté de s.routesImport() :
//
//	s.routesImportServeurs()

// routesImportServeurs enregistre la page et les deux étapes d'analyse et de
// confirmation. Lecture en s.lecteur, analyse/confirmation en s.editeur
// (écriture potentielle, CSRF vérifié).
func (s *serveur) routesImportServeurs() {
	s.mux.HandleFunc("GET /import/serveurs", s.lecteur(s.importServeursPage))
	s.mux.HandleFunc("POST /import/serveurs/analyser", s.editeur(s.importServeursAnalyser))
	s.mux.HandleFunc("POST /import/serveurs/confirmer", s.editeur(s.importServeursConfirmer))
}

func (s *serveur) importServeursPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import_serveurs"), "import_serveurs_page", nil)
}

// importResultatServeurs est le contexte du fragment de résultat, commun aux
// trois cas : erreurs de validation, simulation réussie (jeton à confirmer),
// import réel écrit.
type importResultatServeurs struct {
	importation.Rapport
	Jeton string
}

// colonnesObligatoiresServeur : les trois colonnes toujours requises. Les
// colonnes d'affectation (projet_code/environnement_code/cluster_nom) et de
// rattachement (modele_code) sont facultatives dans l'en-tête lui-même —
// leur présence conjointe est vérifiée ligne à ligne, pas au niveau du
// fichier, puisqu'une ligne peut légitimement ne porter aucune affectation.
var colonnesObligatoiresServeur = []string{"physical_name", "statut", "date_entree"}

// importServeursAnalyser valide entièrement le fichier envoyé, sans jamais
// écrire. En l'absence d'erreur, le contenu brut est déposé dans
// importsEnAttente et son jeton renvoyé au client pour la confirmation —
// l'utilisateur n'a pas à re-choisir son fichier.
func (s *serveur) importServeursAnalyser(w http.ResponseWriter, r *http.Request) {
	fichier, _, err := r.FormFile("fichier")
	if err != nil {
		s.rendreFragment(w, r, "import_serveurs_resultat", importResultatServeurs{
			Rapport: importation.Rapport{Simulation: true, Erreurs: []importation.ErreurLigne{
				{Ligne: 0, Message: "fichier manquant ou illisible : " + err.Error()},
			}},
		})
		return
	}
	defer fichier.Close()

	contenu, err := io.ReadAll(fichier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	rapport, _, err := s.validerImportServeurs(bytes.NewReader(contenu))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	rapport.Simulation = true

	donnees := importResultatServeurs{Rapport: rapport}
	if !rapport.EnErreur() {
		jeton, errDepot := importsEnAttente.Deposer(contenu)
		if errDepot != nil {
			s.erreurServeur(w, r, errDepot)
			return
		}
		donnees.Jeton = jeton
	}
	s.rendreFragment(w, r, "import_serveurs_resultat", donnees)
}

// importServeursConfirmer relit le contenu déposé sous le jeton, le revalide
// entièrement (l'état de la base a pu changer depuis l'analyse — un
// zone ou un cluster peut avoir été retiré), puis écrit si et seulement
// si la revalidation ne remonte toujours aucune erreur. Le jeton est
// consommé dans tous les cas : il ne sert qu'une fois.
func (s *serveur) importServeursConfirmer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	jeton := r.FormValue("jeton")
	contenu, ok := importsEnAttente.Recuperer(jeton)
	defer importsEnAttente.Consommer(jeton)
	if !ok {
		s.rendreFragment(w, r, "import_serveurs_resultat", importResultatServeurs{
			Rapport: importation.Rapport{Erreurs: []importation.ErreurLigne{
				{Ligne: 0, Message: "jeton d'import inconnu ou expiré — recommencer l'analyse"},
			}},
		})
		return
	}

	rapport, resolues, err := s.validerImportServeurs(bytes.NewReader(contenu))
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_serveurs_resultat", importResultatServeurs{Rapport: rapport})
		return
	}

	n, errEcriture := ecrireImportServeurs(s.depotPour(r), resolues)
	if errEcriture != nil {
		s.erreurServeur(w, r, errEcriture)
		return
	}
	rapport.Ecrit = true
	rapport.NbCreees = n
	s.rendreFragment(w, r, "import_serveurs_resultat", importResultatServeurs{Rapport: rapport})
}

// ------------------------------------------------------------- validation

// ligneServeurResolue est une ligne CSV entièrement validée, avec ses
// références résolues en identifiants : prête à être écrite telle quelle.
type ligneServeurResolue struct {
	Numero           int
	Serveur          depot.Serveur
	ClusterID        *int64
	DateAffectation  string
	RevisionID       *int64
	DateRattachement string
}

// cachesImportServeurs évite de refaire, pour chaque ligne, la même requête
// de résolution qu'une ligne précédente a déjà faite (même zone, même
// cluster, même modèle reviennent typiquement sur des centaines de lignes).
type cachesImportServeurs struct {
	zones     map[string]depot.Zone
	clusters  map[string]int64
	revisions map[string]depot.Revision
}

func nouvellesCachesImportServeurs() *cachesImportServeurs {
	return &cachesImportServeurs{
		zones:     map[string]depot.Zone{},
		clusters:  map[string]int64{},
		revisions: map[string]depot.Revision{},
	}
}

// erreurLigneMetier distingue, parmi les erreurs remontées par les lectures
// du dépôt pendant la résolution des références, celles à afficher comme
// anomalie de ligne (référence introuvable) de celles à traiter comme une
// panne inattendue (s.erreurServeur, 500) — même logique que erreurMetier
// pour les écrans CRUD, mais au niveau d'une ligne de CSV plutôt que d'une
// requête HTTP entière.
type erreurLigneMetier struct{ msg string }

func (e erreurLigneMetier) Error() string { return e.msg }

// messageLigneMetier extrait le message si err est une erreurLigneMetier.
func messageLigneMetier(err error) (string, bool) {
	var em erreurLigneMetier
	if errors.As(err, &em) {
		return em.msg, true
	}
	return "", false
}

// enregistrerErreurLigne ajoute le message d'une erreurLigneMetier à msgs et
// renvoie nil pour continuer la validation de la ligne ; pour toute autre
// erreur (panne du dépôt, contexte SQL), la renvoie telle quelle pour que
// l'appelant l'escalade en erreur système.
func enregistrerErreurLigne(err error, msgs *[]string) error {
	if err == nil {
		return nil
	}
	if msg, ok := messageLigneMetier(err); ok {
		*msgs = append(*msgs, msg)
		return nil
	}
	return err
}

// texteOptionnelCSV normalise une valeur de cellule CSV en pointeur, comme
// texteOptionnel le fait pour un formulaire : chaîne vide ou blanche -> nil.
func texteOptionnelCSV(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

// validerImportServeurs décode et valide entièrement le contenu CSV, sans
// jamais écrire. Le retour []ligneServeurResolue n'est significatif que si
// le rapport ne porte aucune erreur (règle du critère d'acceptation v1.4).
// L'erreur de retour ne couvre que les pannes système (lecture du fichier,
// base indisponible) — jamais une anomalie de donnée, qui va dans le rapport.
func (s *serveur) validerImportServeurs(r io.Reader) (importation.Rapport, []ligneServeurResolue, error) {
	entetes, lignes, numeros, err := importation.LireCSV(r)
	if err != nil {
		return importation.Rapport{}, nil, err
	}
	if manquantes := importation.EntetesManquantes(entetes, colonnesObligatoiresServeur); len(manquantes) > 0 {
		return importation.Rapport{
			NbLignes: len(lignes),
			Erreurs: []importation.ErreurLigne{{
				Ligne:   1,
				Message: "colonnes obligatoires manquantes : " + strings.Join(manquantes, ", "),
			}},
		}, nil, nil
	}

	caches := nouvellesCachesImportServeurs()
	rapport := importation.Rapport{NbLignes: len(lignes)}
	var resolues []ligneServeurResolue

	// doublons de nom physique et d'hôte, dans le fichier et contre le réel :
	// ces noms sont les clés de la mise à jour en masse (v3.5), et l'index
	// d'unicité refuserait de toute façon l'écriture — autant nommer la ligne.
	vus := map[string]int{}
	for i, ligne := range lignes {
		resolue, msgs, err := s.validerLigneServeur(ligne, caches)
		if err != nil {
			return importation.Rapport{}, nil, err
		}
		for _, cle := range []struct {
			colonne string
			valeur  *string
		}{
			{"physical_name", resolue.Serveur.PhysicalName}, {"hostname", resolue.Serveur.Hostname},
		} {
			if cle.valeur == nil || *cle.valeur == "" {
				continue
			}
			marque := cle.colonne + "\x1f" + *cle.valeur
			if premiere, deja := vus[marque]; deja {
				msgs = append(msgs, fmt.Sprintf("%s « %s » déjà présent à la ligne %d", cle.colonne, *cle.valeur, premiere))
				continue
			}
			vus[marque] = numeros[i]
			existants, err := s.depot.ServeursParCle(cle.colonne, *cle.valeur)
			if err != nil {
				return importation.Rapport{}, nil, err
			}
			if len(existants) > 0 {
				msgs = append(msgs, fmt.Sprintf("%s « %s » existe déjà (mise à jour : /import/serveurs/maj)", cle.colonne, *cle.valeur))
			}
		}
		if len(msgs) > 0 {
			rapport.Erreurs = append(rapport.Erreurs, importation.ErreurLigne{
				Ligne: numeros[i], Message: strings.Join(msgs, " ; "),
			})
			continue
		}
		resolue.Numero = numeros[i]
		resolues = append(resolues, resolue)
	}
	return rapport, resolues, nil
}

// validerLigneServeur valide une ligne et résout ses références. msgs porte
// toutes les anomalies trouvées sur CETTE ligne (pas seulement la première) :
// un rédacteur corrigeant son fichier doit voir tout ce qui cloche en une
// fois, pas une anomalie par essai. err n'est renseigné que pour une panne
// système survenue pendant une résolution (voir enregistrerErreurLigne).
func (s *serveur) validerLigneServeur(ligne map[string]string, caches *cachesImportServeurs) (ligneServeurResolue, []string, error) {
	var msgs []string

	physicalName := texteOptionnelCSV(ligne["physical_name"])
	if physicalName == nil {
		msgs = append(msgs, "physical_name est obligatoire")
	}

	statut := strings.TrimSpace(ligne["statut"])
	switch statut {
	case depot.StatutHypothese:
		msgs = append(msgs, "HYPOTHESE exige un scénario, non supporté à l'import")
	case depot.StatutCommande, depot.StatutEnService, depot.StatutDecommissionne:
		// valide
	default:
		msgs = append(msgs, fmt.Sprintf("statut « %s » inconnu", statut))
	}

	dateEntree := strings.TrimSpace(ligne["date_entree"])
	if err := depot.ValiderDate("date_entree", dateEntree); err != nil {
		msgs = append(msgs, err.Error())
	}

	// typologie : énumération fixe (depot.Typologies), normalisée en majuscules.
	typologie := texteOptionnelCSV(strings.ToUpper(ligne["typologie"]))
	if !depot.TypologieValide(typologie) {
		msgs = append(msgs, fmt.Sprintf("typologie « %s » inconnue (attendu : %s)", *typologie, strings.Join(depot.Typologies, ", ")))
	}

	dateAffectation := strings.TrimSpace(ligne["date_affectation"])
	if dateAffectation == "" {
		dateAffectation = dateEntree
	} else if err := depot.ValiderDate("date_affectation", dateAffectation); err != nil {
		msgs = append(msgs, err.Error())
	}

	dateRattachement := strings.TrimSpace(ligne["date_rattachement"])
	if dateRattachement == "" {
		dateRattachement = dateEntree
	} else if err := depot.ValiderDate("date_rattachement", dateRattachement); err != nil {
		msgs = append(msgs, err.Error())
	}

	zoneID, err := s.resoudreZoneImport(ligne["zone_code"], caches.zones)
	if e := enregistrerErreurLigne(err, &msgs); e != nil {
		return ligneServeurResolue{}, nil, e
	}

	clusterID, err := s.resoudreClusterImport(ligne["projet_code"], ligne["environnement_code"], ligne["cluster_nom"], caches.clusters)
	if e := enregistrerErreurLigne(err, &msgs); e != nil {
		return ligneServeurResolue{}, nil, e
	}

	revisionID, err := s.resoudreRevisionRecenteImport(ligne["modele_code"], caches.revisions)
	if e := enregistrerErreurLigne(err, &msgs); e != nil {
		return ligneServeurResolue{}, nil, e
	}

	if len(msgs) > 0 {
		return ligneServeurResolue{}, msgs, nil
	}

	dateEntreeCopie := dateEntree
	return ligneServeurResolue{
		Serveur: depot.Serveur{
			PhysicalName:      physicalName,
			Hostname:          texteOptionnelCSV(ligne["hostname"]),
			SerialNumber:      texteOptionnelCSV(ligne["serial_number"]),
			ZoneID:            zoneID,
			PositionZone:      texteOptionnelCSV(ligne["position_zone"]),
			IP:                texteOptionnelCSV(ligne["ip"]),
			VersionOS:         texteOptionnelCSV(ligne["version_os"]),
			Typologie:         typologie,
			CodeAppli:         texteOptionnelCSV(ligne["code_appli"]),
			DemandeRef:        texteOptionnelCSV(ligne["demande_ref"]),
			DemandeServeurRef: texteOptionnelCSV(ligne["demande_serveur_ref"]),
			Statut:            statut,
			DateEntree:        &dateEntreeCopie,
			Commentaire:       texteOptionnelCSV(ligne["commentaire"]),
		},
		ClusterID:        clusterID,
		DateAffectation:  dateAffectation,
		RevisionID:       revisionID,
		DateRattachement: dateRattachement,
	}, nil, nil
}

// resoudreZoneImport résout un zone_code facultatif. Absent ->
// (nil, nil), le serveur importé n'a simplement pas de zone renseignée.
func (s *serveur) resoudreZoneImport(code string, cache map[string]depot.Zone) (*int64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil
	}
	if dc, ok := cache[code]; ok {
		id := dc.ID
		return &id, nil
	}
	dc, err := s.depot.LireZoneParCode(code)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			return nil, erreurLigneMetier{fmt.Sprintf("zone « %s » introuvable", code)}
		}
		return nil, err
	}
	cache[code] = dc
	id := dc.ID
	return &id, nil
}

// resoudreClusterImport applique la règle « les trois ensemble ou aucun » :
// zéro colonne renseignée -> pas d'affectation (nil, nil) ; une ou deux ->
// erreur de ligne ; les trois -> résolution par jointure sur les codes,
// erreur de ligne nommant les codes fournis si aucun cluster ne correspond.
func (s *serveur) resoudreClusterImport(projetCode, environnementCode, clusterNom string, cache map[string]int64) (*int64, error) {
	projetCode = strings.TrimSpace(projetCode)
	environnementCode = strings.TrimSpace(environnementCode)
	clusterNom = strings.TrimSpace(clusterNom)

	nbRenseignes := 0
	for _, v := range []string{projetCode, environnementCode, clusterNom} {
		if v != "" {
			nbRenseignes++
		}
	}
	if nbRenseignes == 0 {
		return nil, nil
	}
	if nbRenseignes != 3 {
		return nil, erreurLigneMetier{
			"projet_code, environnement_code et cluster_nom doivent être renseignés tous les trois, ou aucun",
		}
	}

	cle := projetCode + "\x00" + environnementCode + "\x00" + clusterNom
	if id, ok := cache[cle]; ok {
		return &id, nil
	}

	var id int64
	err := s.depot.Base().QueryRow(
		`SELECT c.id FROM cluster c
		 JOIN projet p ON p.id = c.projet_id
		 JOIN environnement e ON e.id = c.environnement_id
		 WHERE p.code = ? AND e.code = ? AND c.nom = ?`,
		projetCode, environnementCode, clusterNom).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, erreurLigneMetier{fmt.Sprintf(
				"cluster introuvable pour projet_code « %s », environnement_code « %s », cluster_nom « %s »",
				projetCode, environnementCode, clusterNom)}
		}
		return nil, err
	}
	cache[cle] = id
	return &id, nil
}

// resoudreRevisionRecenteImport résout un modele_code facultatif vers la
// révision de numéro le plus élevé de ce modèle. Absent -> (nil, nil), pas de
// rattachement à créer. ListerRevisions trie par numéro croissant : la
// dernière de la liste est la plus récente.
func (s *serveur) resoudreRevisionRecenteImport(modeleCode string, cache map[string]depot.Revision) (*int64, error) {
	modeleCode = strings.TrimSpace(modeleCode)
	if modeleCode == "" {
		return nil, nil
	}
	if rev, ok := cache[modeleCode]; ok {
		id := rev.ID
		return &id, nil
	}

	m, err := s.depot.LireModeleParCode(modeleCode)
	if err != nil {
		if errors.Is(err, depot.ErrIntrouvable) {
			return nil, erreurLigneMetier{fmt.Sprintf("modèle « %s » introuvable", modeleCode)}
		}
		return nil, err
	}
	revisions, err := s.depot.ListerRevisions(m.ID)
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, erreurLigneMetier{fmt.Sprintf("modèle « %s » n'a aucune révision", modeleCode)}
	}
	rev := revisions[len(revisions)-1]
	cache[modeleCode] = rev
	id := rev.ID
	return &id, nil
}

// ------------------------------------------------------------------ écriture

// ecrireImportServeurs écrit toutes les lignes résolues dans une seule
// transaction : soit tout est créé, soit rien ne l'est (critère d'acceptation
// v1.4). N'est appelé qu'après une revalidation à zéro erreur.
func ecrireImportServeurs(d *depot.Depot, resolues []ligneServeurResolue) (int, error) {
	tx, err := d.Base().Begin()
	if err != nil {
		return 0, fmt.Errorf("ouverture de la transaction d'import des serveurs : %w", err)
	}

	n, err := ecrireLignesServeursImport(d, tx, resolues)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("import des serveurs : %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("validation de l'import des serveurs : %w", err)
	}
	return n, nil
}

// ecrireLignesServeursImport porte la logique d'écriture proprement dite,
// séparée de l'ouverture/fermeture de la transaction pour rester testable et
// pour que l'appelant contrôle le rollback en un seul endroit.
func ecrireLignesServeursImport(d *depot.Depot, tx *sql.Tx, resolues []ligneServeurResolue) (int, error) {
	for _, l := range resolues {
		srv := l.Serveur
		res, err := tx.Exec(
			`INSERT INTO serveur
			 (physical_name, hostname, serial_number, zone_id, position_zone, ip,
			  version_os, typologie, code_appli, demande_ref, demande_serveur_ref,
			  statut, scenario_id, date_entree, commentaire)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,NULL,?,?)`,
			srv.PhysicalName, srv.Hostname, srv.SerialNumber, srv.ZoneID, srv.PositionZone, srv.IP,
			srv.VersionOS, srv.Typologie, srv.CodeAppli, srv.DemandeRef, srv.DemandeServeurRef,
			srv.Statut, srv.DateEntree, srv.Commentaire)
		if err != nil {
			return 0, err
		}
		serveurID, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		if err := d.JournaliserCreation(tx, "serveur", "serveur", serveurID); err != nil {
			return 0, err
		}

		if l.RevisionID != nil {
			resRatt, err := tx.Exec(
				`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, ?, ?)`,
				serveurID, *l.RevisionID, l.DateRattachement)
			if err != nil {
				return 0, err
			}
			rattID, _ := resRatt.LastInsertId()
			if err := d.JournaliserCreation(tx, "serveur_revision", "serveur_revision", rattID); err != nil {
				return 0, err
			}
		}
		if l.ClusterID != nil {
			resAff, err := tx.Exec(
				`INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id) VALUES (?, ?, ?, NULL)`,
				serveurID, *l.ClusterID, l.DateAffectation)
			if err != nil {
				return 0, err
			}
			affID, _ := resAff.LastInsertId()
			if err := d.JournaliserCreation(tx, "affectation", "affectation", affID); err != nil {
				return 0, err
			}
		}
	}
	return len(resolues), nil
}
