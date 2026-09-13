-- Parallax — v3.6 : authentification LDAP (backlog v3.6, entretien F)
--
-- Origine d'un compte : LOCAL (mot de passe argon2id en base) ou LDAP
-- (vérifié par bind simple sur l'annuaire, hash vide, jamais stocké). Les
-- comptes existants sont tous locaux. Le rôle reste porté par Parallax
-- quelle que soit l'origine.
ALTER TABLE utilisateur ADD COLUMN origine TEXT NOT NULL DEFAULT 'LOCAL'
    CHECK (origine IN ('LOCAL', 'LDAP'));
