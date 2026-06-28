package migrations

var V39TeamMemberRoles = Migration{
	Name: "TeamMemberRoles",
	SQL: "ALTER TABLE teams_users ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'maintain'; UPDATE teams_users SET role = 'admin' WHERE admin = true; UPDATE teams_users SET role = 'maintain' WHERE admin = false;",
}
