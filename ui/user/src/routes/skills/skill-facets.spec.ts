import { labelFromSlug, skillFacets, toSkillRows } from './skill-facets';
import { describe, expect, it } from 'vitest';

describe('labelFromSlug', () => {
	it('title-cases slugs and upper-cases known acronyms', () => {
		expect(labelFromSlug('payables-receivables')).toBe('Payables Receivables');
		expect(labelFromSlug('seo')).toBe('SEO');
		expect(labelFromSlug('ai')).toBe('AI');
		expect(labelFromSlug('')).toBe('');
	});
});

describe('skillFacets', () => {
	it('reads department and group from skills/<department>/<group>/<skill>', () => {
		expect(
			skillFacets({ relativePath: 'skills/accounting/tax/sales-tax-vat-gst-compliance' })
		).toEqual({ department: 'Accounting', group: 'Tax' });
		expect(skillFacets({ relativePath: 'skills/seo/ai-search/seo-ai-search-visibility' })).toEqual({
			department: 'SEO',
			group: 'AI Search'
		});
	});

	it('keeps the extra level for platform skills', () => {
		expect(
			skillFacets({ relativePath: 'skills/accounting/platforms/xero/xero-month-end-close' })
		).toEqual({ department: 'Accounting', group: 'Platforms / Xero' });
	});

	it('works without the leading skills/ folder', () => {
		expect(skillFacets({ relativePath: 'engineering/devops/terraform-patterns' })).toEqual({
			department: 'Engineering',
			group: 'Devops'
		});
	});

	it('falls back to metadata for a skill that is not in a department folder', () => {
		expect(
			skillFacets({
				relativePath: 'skills/lonely-skill',
				metadata: { department: 'marketing', domain: 'email' }
			})
		).toEqual({ department: 'Marketing', group: 'Email' });
	});

	it('always returns something to filter on', () => {
		expect(skillFacets({})).toEqual({ department: 'Other', group: 'General' });
		expect(skillFacets({ relativePath: 'only-a-skill' })).toEqual({
			department: 'Other',
			group: 'General'
		});
	});

	it('ignores non-string metadata', () => {
		expect(skillFacets({ relativePath: 'x', metadata: { department: 5, domain: null } })).toEqual({
			department: 'Other',
			group: 'General'
		});
	});
});

describe('toSkillRows', () => {
	it('adds department and group without losing the skill fields', () => {
		const [row] = toSkillRows([
			{
				id: 'sk-1',
				type: 'skill',
				name: 'dcf-model',
				relativePath: 'skills/accounting/advisory/dcf-model'
			}
		]);
		expect(row).toMatchObject({
			id: 'sk-1',
			name: 'dcf-model',
			department: 'Accounting',
			group: 'Advisory'
		});
	});
});

describe('future folders', () => {
	it('turns folders nobody has created yet into department and group values', () => {
		const rows = toSkillRows([
			{ id: '1', type: 'skill', relativePath: 'skills/hr/recruiting/screen-candidates' },
			{ id: '2', type: 'skill', relativePath: 'skills/finops/cloud-cost/rightsizing' },
			{ id: '3', type: 'skill', relativePath: 'skills/hr/recruiting/senior/interview-loop' }
		]);
		expect(rows.map((r) => [r.department, r.group])).toEqual([
			['HR', 'Recruiting'],
			['Finops', 'Cloud Cost'],
			['HR', 'Recruiting / Senior']
		]);
		// the table builds its filter options from these values, so the two departments appear without any code change
		expect(new Set(rows.map((r) => r.department))).toEqual(new Set(['HR', 'Finops']));
	});
});
