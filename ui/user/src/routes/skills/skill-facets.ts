import type { Skill } from '$lib/services/nanobot/types';

export type SkillRow = Skill & { department: string; group: string };

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
	return skills.map((skill) => ({ ...skill, ...skillFacets(skill) }));
}
