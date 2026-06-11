package migrations

var V34WorkerTags = Migration{
	Name: "AddTagsColumns",
	SQL: `ALTER TABLE workers ADD COLUMN tags VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE workers ADD COLUMN exclusive_tags BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE jobs ADD COLUMN tags VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE resources ADD COLUMN tags VARCHAR(255) NOT NULL DEFAULT '';`,
}
