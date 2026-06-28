package migrations

var V42RenameRoles = Migration{
	Name: "RenameRoles",
	SQL: `UPDATE teams_users SET role = 'read' WHERE role = 'viewer';
UPDATE teams_users SET role = 'write' WHERE role = 'operator';
UPDATE teams_users SET role = 'maintain' WHERE role = 'maintainer';
UPDATE api_tokens SET role = 'read' WHERE role = 'viewer';
UPDATE api_tokens SET role = 'write' WHERE role = 'operator';
UPDATE api_tokens SET role = 'maintain' WHERE role = 'maintainer';`,
}
