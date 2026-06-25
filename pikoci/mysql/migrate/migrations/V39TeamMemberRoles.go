package migrations

var V39TeamMemberRoles = Migration{
	Name: "TeamMemberRoles",
	SQL: "ALTER TABLE teams_users ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'maintainer'; UPDATE teams_users SET role = 'admin' WHERE admin = 1; UPDATE teams_users SET role = 'maintainer' WHERE admin = 0;",
}
