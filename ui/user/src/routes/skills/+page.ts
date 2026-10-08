import { handleRouteError, HttpError } from '$lib/errors';
import { NanobotService } from '$lib/services';
import type { Skill } from '$lib/services/nanobot/types';
import type { PageLoad } from './$types';

const SKILL_LIST_LIMIT = 1000; // the server clamps this to its own maximum

export const load: PageLoad = async ({ fetch, parent }) => {
	const { profile } = await parent();
	let skills: Skill[] = [];
	let showLicenseError = false;

	try {
		// Ask for the whole library: the page filters and searches in the browser, so it needs every skill the user may see.
		skills = await NanobotService.listSkills({
			fetch,
			limit: SKILL_LIST_LIMIT,
			dontLogErrors: true
		});
	} catch (err) {
		if (err instanceof HttpError && err.statusCode === 402) {
			skills = [];
			showLicenseError = true;
		} else {
			handleRouteError(err, `/skills`, profile);
		}
	}

	return {
		skills,
		showLicenseError
	};
};
