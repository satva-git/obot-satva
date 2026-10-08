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
