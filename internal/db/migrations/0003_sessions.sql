-- Parallax — sessions d'authentification locale
--
-- Sessions côté serveur plutôt que jeton auto-porteur : révocation immédiate
-- possible (déconnexion, désactivation d'un compte), et la base est déjà là.
-- Le jeton de session est un identifiant opaque, jamais l'identité elle-même.

CREATE TABLE session (
    id                TEXT    PRIMARY KEY,  -- jeton aléatoire (32 octets, base64url)
    utilisateur_id    INTEGER NOT NULL REFERENCES utilisateur (id) ON DELETE CASCADE,
    csrf_token        TEXT    NOT NULL,     -- jeton synchronizer, distinct du jeton de session
    cree_le           TEXT    NOT NULL,
    expire_le         TEXT    NOT NULL,     -- durée de vie absolue
    derniere_activite TEXT    NOT NULL      -- pour l'expiration glissante
);

CREATE INDEX idx_session_utilisateur ON session (utilisateur_id);
CREATE INDEX idx_session_expire ON session (expire_le);
