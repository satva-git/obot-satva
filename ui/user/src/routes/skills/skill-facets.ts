import type { Skill } from '$lib/services/nanobot/types';

export type SkillRow = Skill & { department: string; group: string; folder: string[] };

/** The only two fields the facets need, so the helper is easy to test and reuse. */
export type SkillLocation = { relativePath?: string; metadata?: Record<string, unknown> };

const ACRONYMS = new Set([
	'ai',
	'seo',
	'ui',
	'ux',
	'qa',
	'pr',
	'cro',
	'gtm',
	'sql',
	'api',
	'dbt',
	'it',
	'hr'
]);

/** "payables-receivables" -> "Payables Receivables", "seo" -> "SEO". */
export function labelFromSlug(slug: string): string {
	return slug
		.split(/[-_\s]+/)
		.filter(Boolean)
		.map((word) =>
			ACRONYMS.has(word.toLowerCase())
				? word.toUpperCase()
				: word.charAt(0).toUpperCase() + word.slice(1)
		)
		.join(' ');
}

function metadataText(skill: SkillLocation, key: string): string {
	const value = skill.metadata?.[key];
	return typeof value === 'string' ? value.trim() : '';
}

/**
 * Department and group of a skill, so a large shared library can be narrowed down on the Skills page.
 *
 * Libraries are laid out `skills/<department>/<group>/<skill>`, and some nest one level deeper
 * (`skills/accounting/platforms/<system>/<skill>`). The folders above the skill folder give the department
 * (first) and group (the rest, joined). A skill outside that layout falls back to its own metadata
 * (`department`, `domain`), and finally to "Other" / "General", so every skill always has a value to filter on.
 */
export function skillFacets(skill: SkillLocation): {
	department: string;
	group: string;
} {
	const parts = (skill.relativePath ?? '').split('/').filter(Boolean);
	const inside = parts[0] === 'skills' ? parts.slice(1) : parts;
	const folders = inside.slice(0, -1); // drop the skill's own folder

	const department = folders[0] ?? (metadataText(skill, 'department') || 'other');
	const group =
		folders.length > 1
			? folders.slice(1).map(labelFromSlug).join(' / ')
			: labelFromSlug(metadataText(skill, 'domain'));

	return { department: labelFromSlug(department), group: group || 'General' };
}

export function toSkillRows(skills: Skill[]): SkillRow[] {
	return skills.map((skill) => {
		const facets = skillFacets(skill);
		return { ...skill, ...facets, folder: folderOf(facets) };
	});
}

/** The skill's folder as labels, e.g. ["Accounting", "Platforms", "Xero"]; "General" is no real folder. */
function folderOf({ department, group }: { department: string; group: string }): string[] {
	return group === 'General' ? [department] : [department, ...group.split(' / ')];
}

export type FolderNode = { name: string; path: string; count: number; children: FolderNode[] };

/** Folder tree for the Skills folder browser; `path` is the labels joined by "/", counts include subfolders. */
export function buildFolderTree(rows: { folder: string[] }[]): FolderNode {
	const root: FolderNode = { name: 'All', path: '', count: 0, children: [] };
	for (const row of rows) {
		let node = root;
		node.count++;
		row.folder.forEach((name, i) => {
			let child = node.children.find((c) => c.name === name);
			if (!child) {
				child = { name, path: row.folder.slice(0, i + 1).join('/'), count: 0, children: [] };
				node.children.push(child);
			}
			child.count++;
			node = child;
		});
	}
	const sort = (node: FolderNode) => {
		node.children.sort((a, b) => a.name.localeCompare(b.name));
		node.children.forEach(sort);
	};
	sort(root);
	return root;
}

export function findFolder(root: FolderNode, path: string): FolderNode | undefined {
	if (!path) return root;
	return path
		.split('/')
		.reduce<
			FolderNode | undefined
		>((node, name) => node?.children.find((c) => c.name === name), root);
}

/** Is the row in the folder at `path`? The root ("") holds everything. */
export function inFolder(
	row: { folder: string[] },
	path: string,
	includeSubfolders: boolean
): boolean {
	if (!path) return true;
	const parts = path.split('/');
	if (!parts.every((name, i) => row.folder[i] === name)) return false;
	return includeSubfolders || row.folder.length === parts.length;
}
