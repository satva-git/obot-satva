<script lang="ts">
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { tooltip } from '$lib/actions/tooltip.svelte';
	import CopyField from '$lib/components/CopyField.svelte';
	import Layout from '$lib/components/Layout.svelte';
	import ResponsiveDialog from '$lib/components/ResponsiveDialog.svelte';
	import Search from '$lib/components/Search.svelte';
	import MarkdownEditor from '$lib/components/nanobot/MarkdownEditor.svelte';
	import Table from '$lib/components/table/Table.svelte';
	import { sanitizeFilenameSegment, saveBlob } from '$lib/download';
	import Loading from '$lib/icons/Loading.svelte';
	import { AdminService, UserService } from '$lib/services';
	import type { Skill } from '$lib/services/nanobot/types';
	import {
		AiClient,
		COMMON_AI_CLIENTS_MAP,
		MCP_CONNECTION_INVALID_LICENSE_MESSAGE
	} from '$lib/services/user/constants';
	import { formatTimeAgo } from '$lib/time';
	import {
		clearUrlParams,
		getTableUrlParamsFilters,
		setFilterUrlParams,
		setUrlParamAndUpdateUrl
	} from '$lib/url.js';
	import {
		buildFolderTree,
		findFolder,
		inFolder,
		toSkillRows,
		type FolderNode
	} from './skill-facets';
	import {
		TriangleAlert,
		PencilRuler,
		Bot,
		Download,
		ChevronDown,
		ChevronRight,
		Folder,
		Library
	} from '@lucide/svelte';
	import { untrack } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { twMerge } from 'tailwind-merge';

	let { data } = $props();
	let query = $derived(page.url.searchParams.get('query') ?? '');

	let skills = $state<Skill[]>(untrack(() => data?.skills ?? []));
	// Each skill also carries a department and group (from its folder path) so a large library can be narrowed down.
	let skillRows = $derived(toSkillRows(skills));
	// `folder` belongs to the folder browser, not to a table column.
	let urlFilters = $derived(
		Object.fromEntries(
			Object.entries(getTableUrlParamsFilters()).filter(([key]) => key !== 'folder')
		)
	);

	// Folder browser: the skill library's folders as a tree; the selected folder lives in the URL (?folder=).
	let folderTree = $derived(buildFolderTree(skillRows));
	let folderPath = $derived(page.url.searchParams.get('folder') ?? '');
	let currentFolder = $derived(findFolder(folderTree, folderPath) ?? folderTree);
	let includeSubfolders = $state(true);
	const openFolders = new SvelteSet<string>(ancestorsOf(untrack(() => folderPath)));

	function ancestorsOf(path: string): string[] {
		return path ? path.split('/').map((_, i, parts) => parts.slice(0, i + 1).join('/')) : [];
	}

	function selectFolder(path: string) {
		// Only the selected branch stays open, so the tree stays short.
		openFolders.clear();
		ancestorsOf(path).forEach((p) => openFolders.add(p));
		setUrlParamAndUpdateUrl(page.url, 'folder', path);
	}

	function toggleFolder(path: string) {
		if (openFolders.has(path)) openFolders.delete(path);
		else openFolders.add(path);
	}

	let copyFields = $state<ReturnType<typeof CopyField>[]>([]);
	let installSkillDialog = $state<ReturnType<typeof ResponsiveDialog> | undefined>(undefined);
	let selectedSkillToInstall = $state<Skill | undefined>(undefined);
	let viewSkillDialog = $state<ReturnType<typeof ResponsiveDialog> | undefined>(undefined);
	let viewedSkill = $state<Skill | undefined>(undefined);
	let viewedSkillMarkdown = $state('');
	let viewedSkillLoading = $state(false);

	async function viewSkill(skill: Skill) {
		viewedSkill = skill;
		viewedSkillMarkdown = '';
		viewedSkillLoading = true;
		viewSkillDialog?.open();
		try {
			viewedSkillMarkdown = await AdminService.getSkillPreview(skill.id);
		} catch (err) {
			console.error('Failed to load skill preview', err);
			viewedSkillMarkdown = 'Could not load this skill. Please try again.';
		} finally {
			viewedSkillLoading = false;
		}
	}

	let selectedTab = $state<'macos/linux' | 'windows'>('windows');

	// One-command install: a short-lived token lets curl/irm fetch the private skill with no login.
	let installToken = $state<string | undefined>(undefined);
	let installTokenError = $state('');

	async function loadInstallToken() {
		const skill = selectedSkillToInstall;
		installToken = undefined;
		installTokenError = '';
		if (!skill) return;
		try {
			const { token } = await UserService.createSkillInstallToken(skill.id);
			if (selectedSkillToInstall?.id === skill.id) installToken = token;
		} catch (err) {
			console.error('Failed to create skill install token', err);
			if (selectedSkillToInstall?.id === skill.id) {
				installTokenError = 'Could not create the install command. Use the steps by hand below.';
			}
		}
	}

	let clientUnzipCommands = $derived.by(() => {
		const skillName = sanitizeFilenameSegment(
			selectedSkillToInstall?.name ?? selectedSkillToInstall?.id ?? 'skill'
		);
		const command = (clientSkillsDirectory: string) => {
			if (selectedTab === 'windows') {
				const windowsSkillsDirectory = clientSkillsDirectory.replaceAll('/', '\\');
				// PowerShell, the default Windows terminal; Expand-Archive creates the folder itself.
				return `Expand-Archive -Force "$env:USERPROFILE\\Downloads\\${skillName}.zip" "$env:USERPROFILE\\${windowsSkillsDirectory}\\${skillName}"`;
			}

			return `unzip "$HOME/Downloads/${skillName}.zip" -d "$HOME/${clientSkillsDirectory}/${skillName}"`;
		};

		// One command that downloads and installs; needs nothing but the built-in terminal.
		const oneCommand = (clientSkillsDirectory: string) => {
			const url = `${page.url.origin}/api/skill-install/${installToken ?? ''}`;
			return selectedTab === 'windows'
				? `irm "${url}/windows?dir=${clientSkillsDirectory}" | iex`
				: `curl -fsSL "${url}/unix?dir=${clientSkillsDirectory}" | sh`;
		};

		return [
			{
				id: 'cursor',
				label: 'Cursor',
				icon: COMMON_AI_CLIENTS_MAP.get(AiClient.Cursor)?.icon,
				iconDark: COMMON_AI_CLIENTS_MAP.get(AiClient.Cursor)?.iconDark,
				command: command('.cursor/skills'),
				oneCommand: oneCommand('.cursor/skills')
			},
			{
				id: 'claudeCode',
				label: 'Claude Code',
				icon: COMMON_AI_CLIENTS_MAP.get(AiClient.Claude)?.icon,
				iconDark: COMMON_AI_CLIENTS_MAP.get(AiClient.Claude)?.iconDark,
				command: command('.claude/skills'),
				oneCommand: oneCommand('.claude/skills')
			},
			{
				id: 'codex',
				label: 'Codex',
				icon: COMMON_AI_CLIENTS_MAP.get(AiClient.Codex)?.icon,
				iconDark: COMMON_AI_CLIENTS_MAP.get(AiClient.Codex)?.iconDark,
				command: command('.codex/skills'),
				oneCommand: oneCommand('.codex/skills')
			},
			{
				id: 'other',
				label: 'Other',
				command: command('.agents/skills'),
				oneCommand: oneCommand('.agents/skills')
			}
		];
	});

	let folderRows = $derived(
		skillRows.filter((row) => inFolder(row, currentFolder.path, includeSubfolders))
	);

	let skillsTableData = $derived(
		query
			? folderRows.filter(
					(d) =>
						d.displayName?.toLowerCase().includes(query.toLowerCase()) ||
						d.name?.toLowerCase().includes(query.toLowerCase()) ||
						d.description?.toLowerCase().includes(query.toLowerCase()) ||
						d.department.toLowerCase().includes(query.toLowerCase()) ||
						d.group.toLowerCase().includes(query.toLowerCase())
				)
			: folderRows
	);

	function updateSearchQuery(value: string) {
		setUrlParamAndUpdateUrl(page.url, 'query', value);
	}

	async function handleDownloadSkill(skill?: Skill) {
		if (!skill) return;
		try {
			const blob = await UserService.downloadSkill(skill.id);
			const filename = `${sanitizeFilenameSegment(skill.name ?? skill.id)}.zip`;
			saveBlob(blob, filename);
		} catch (err) {
			console.error('Failed to download skill', err);
		}
	}
</script>

<Layout classes={{ navbar: 'bg-base-200', container: 'pt-0' }} title="Skills">
	{#snippet rightNavActions()}
		<a class="btn btn-primary" href={resolve('/install-cli')}>Get Obot CLI</a>
	{/snippet}
	<div class="flex min-h-full flex-col gap-2">
		<div class="flex min-h-full flex-col">
			<div class="bg-base-200 dark:bg-base-100 sticky top-16 left-0 z-20 w-full py-1">
				<div class="mb-2">
					<Search
						class="dark:bg-base-200 dark:border-base-400 bg-base-100 border border-transparent shadow-sm"
						value={query}
						onChange={updateSearchQuery}
						placeholder={currentFolder.path
							? `Search ${currentFolder.count} skills in ${currentFolder.name}...`
							: `Search ${skills.length} skills...`}
					/>
				</div>
			</div>

			<div class="dark:bg-base-300 bg-base-100 rounded-t-md shadow-sm">
				{@render skillsView()}
			</div>
		</div>
	</div>
</Layout>

{#snippet folderBranch(nodes: FolderNode[], depth: number)}
	{#each nodes as node (node.path)}
		<div
			class={twMerge(
				'hover:bg-base-200 dark:hover:bg-base-400 flex items-center gap-1 rounded-md pr-2 text-sm',
				currentFolder.path === node.path && 'bg-base-200 dark:bg-base-400 font-medium'
			)}
			style={`padding-left: ${depth * 14}px`}
		>
			<button
				class="btn btn-ghost btn-xs btn-square"
				aria-label={openFolders.has(node.path) ? `Collapse ${node.name}` : `Expand ${node.name}`}
				aria-expanded={openFolders.has(node.path)}
				disabled={!node.children.length}
				onclick={() => toggleFolder(node.path)}
			>
				{#if node.children.length}
					{#if openFolders.has(node.path)}
						<ChevronDown class="size-3" />
					{:else}
						<ChevronRight class="size-3" />
					{/if}
				{/if}
			</button>
			<button
				class="flex min-w-0 flex-1 items-center gap-2 py-1.5 text-left"
				onclick={() => selectFolder(node.path)}
			>
				<Folder class="size-4 shrink-0" />
				<span class="truncate">{node.name}</span>
				<span class="text-muted-content ml-auto text-xs">{node.count}</span>
			</button>
		</div>
		{#if openFolders.has(node.path)}
			{@render folderBranch(node.children, depth + 1)}
		{/if}
	{/each}
{/snippet}

{#snippet skillsView()}
	<div class="flex flex-col gap-2">
		{#if data?.showLicenseError}
			<div class="my-12 flex w-md flex-col items-center gap-4 self-center text-center">
				<TriangleAlert class="size-12 text-warning" />
				<h4 class="text-muted-content text-lg font-semibold">Limited Functionality</h4>
				<p class="text-muted-content text-sm font-light">
					{MCP_CONNECTION_INVALID_LICENSE_MESSAGE}
				</p>
			</div>
		{:else if skills.length > 0}
			<div class="flex">
				<!-- The tree stays in view while the skill list next to it scrolls with the page. -->
				<nav
					aria-label="Skill folders"
					class="border-base-300 sticky top-36 hidden max-h-[calc(100vh-10rem)] w-64 shrink-0 self-start overflow-y-auto border-r p-2 md:block"
				>
					<button
						class={twMerge(
							'hover:bg-base-200 dark:hover:bg-base-400 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm',
							!currentFolder.path && 'bg-base-200 dark:bg-base-400 font-medium'
						)}
						onclick={() => selectFolder('')}
					>
						<Library class="size-4 shrink-0" />
						<span>All skills</span>
						<span class="text-muted-content ml-auto text-xs">{folderTree.count}</span>
					</button>
					{@render folderBranch(folderTree.children, 0)}
				</nav>
				<div class="flex min-w-0 flex-1 flex-col">
					<div class="flex flex-col gap-2 p-3">
						<div class="flex flex-wrap items-center gap-1 text-sm">
							<button class="link link-hover" onclick={() => selectFolder('')}>All skills</button>
							{#each ancestorsOf(currentFolder.path) as path (path)}
								<ChevronRight class="text-muted-content size-3" />
								<button class="link link-hover" onclick={() => selectFolder(path)}>
									{path.split('/').at(-1)}
								</button>
							{/each}
							{#if currentFolder.path && currentFolder.children.length}
								<label class="ml-auto flex cursor-pointer items-center gap-2">
									<input
										type="checkbox"
										class="checkbox checkbox-sm"
										bind:checked={includeSubfolders}
									/>
									Include subfolders
								</label>
							{/if}
						</div>
						{#if currentFolder.children.length}
							<div class="flex flex-wrap gap-1.5">
								{#each currentFolder.children as child (child.path)}
									<button
										class="border-base-300 hover:bg-base-200 dark:hover:bg-base-400 flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs"
										onclick={() => selectFolder(child.path)}
									>
										<Folder class="size-3.5 shrink-0" />
										{child.name}
										<span class="text-muted-content">{child.count}</span>
									</button>
								{/each}
							</div>
						{/if}
					</div>
					<Table
						data={skillsTableData}
						fields={['displayName', 'department', 'group', 'description', 'created']}
						noDataMessage="No skills found."
						classes={{
							root: 'rounded-none rounded-b-md shadow-none'
						}}
						columnMaxWidths={{ created: 240, department: 160, group: 200 }}
						sortable={['displayName', 'department', 'group', 'created']}
						filterable={['department', 'group']}
						filters={urlFilters}
						onFilter={setFilterUrlParams}
						onClearAllFilters={() => clearUrlParams(['department', 'group'])}
						headers={[
							{
								title: 'Name',
								property: 'displayName'
							},
							{ title: 'Department', property: 'department' },
							{ title: 'Group', property: 'group' }
						]}
						setRowClasses={(d) => {
							if (d.validationError) {
								return 'opacity-50 cursor-default dark:hover:bg-transparent hover:bg-transparent';
							}
							return '';
						}}
					>
						{#snippet onRenderColumn(property, d)}
							{#if property === 'displayName'}
								<span class="flex items-center gap-2">
									{d.displayName}
									{#if d.validationError}
										<div use:tooltip={{ text: d.validationError }}>
											<TriangleAlert class="size-3 text-warning" />
										</div>
									{/if}
								</span>
							{:else if property === 'created'}
								{formatTimeAgo(d.created).relativeTime}
							{:else if property === 'description'}
								<span class="line-clamp-2">{d.description ?? '—'}</span>
							{:else}
								{d[property as keyof typeof d]}
							{/if}
						{/snippet}
						{#snippet actions(d)}
							<div id={`install-skill-btn-container-${d.id}`} class="flex gap-2">
								<button class="btn btn-sm" onclick={() => viewSkill(d)}>View</button>
								<button
									class="btn btn-primary btn-sm"
									id={`install-skill-btn-${d.id}`}
									onclick={() => {
										selectedSkillToInstall = d;
										installSkillDialog?.open();
									}}
								>
									Install
								</button>
							</div>
						{/snippet}
					</Table>
				</div>
			</div>
		{:else}
			<div class="my-12 flex w-md flex-col items-center gap-4 self-center text-center">
				<PencilRuler class="text-base-content/80 size-24" />
				<h4 class="text-muted-content text-lg font-semibold">No current skills.</h4>
				<p class="text-muted-content text-sm font-light">
					Once a Git Source URL has been added, the skills <br />
					discovered will be viewable from here.
				</p>
			</div>
		{/if}
	</div>
{/snippet}

<ResponsiveDialog
	bind:this={installSkillDialog}
	onOpen={loadInstallToken}
	animate="slide"
	id="install-skill-dialog"
	title={selectedSkillToInstall?.displayName}
>
	<div class="w-full @container md:px-0 px-4">
		<div class="relative">
			<p class="absolute top-1/2 -translate-y-1/2 left-2 text-xs font-semibold">Choose your OS:</p>
			<div
				id="install-skill-os-selector"
				role="tablist"
				class="tabs tabs-box tabs-sm flex items-center justify-end mb-1"
			>
				<button
					role="tab"
					class={twMerge('tab', selectedTab === 'windows' && 'tab-active')}
					onclick={() => (selectedTab = 'windows')}
				>
					Windows (PowerShell)
				</button>
				<button
					role="tab"
					class={twMerge('tab', selectedTab === 'macos/linux' && 'tab-active')}
					onclick={() => (selectedTab = 'macos/linux')}
				>
					macOS/Linux
				</button>
			</div>
		</div>
		<div class="divider">Install with one command (paste into your terminal)</div>
		{#if installTokenError}
			<p class="text-error mb-2 text-sm">{installTokenError}</p>
		{:else if !installToken}
			<Loading />
		{:else}
			<p class="text-muted-content mb-2 text-xs">Link works for 15 minutes.</p>
			<div class="flex flex-col gap-2">
				{#each clientUnzipCommands as client (client.id)}
					<CopyField
						value={client.oneCommand}
						id={`one-command-${client.id}`}
						classes={{ inputLabel: 'bg-base-100 dark:bg-base-300', input: 'font-mono' }}
					>
						{#snippet preContent()}
							<span class="label shrink-0 w-38 mr-0 text-base-content">{client.label}</span>
						{/snippet}
					</CopyField>
				{/each}
			</div>
		{/if}
		<div id="download-skill-container">
			<div class="divider">Or by hand: 1. Download {selectedSkillToInstall?.displayName}</div>
			<div class="md:p-0 md:pb-0 p-4">
				<button
					class="btn btn-primary btn-sm w-full"
					onclick={() => handleDownloadSkill(selectedSkillToInstall)}
				>
					<Download class="size-4" /> Download
				</button>
			</div>
		</div>
		<div class="divider">2. Unzip via CLI</div>
		<div id="unzip-skill-commands-container" class="flex gap-2 flex-col">
			{#each clientUnzipCommands as client, index (client.id)}
				<div id={`unzip-skill-command-${client.id}-container`}>
					<CopyField
						value={client.command}
						id={`command-${client.id}`}
						classes={{
							inputLabel: 'bg-base-100 dark:bg-base-300',
							input: 'font-mono'
						}}
						bind:this={copyFields[index]}
					>
						{#snippet preContent()}
							<span class="label shrink-0 w-38 mr-0 text-base-content">
								{#if client.icon || client.iconDark}
									<img
										src={client.iconDark ?? client.icon}
										alt={`${client.label} branding icon`}
										class="size-4 dark:block hidden"
									/>
									<img
										src={client?.icon}
										alt={`${client.label} branding icon`}
										class="size-4 block dark:hidden"
									/>
								{:else}
									<Bot class="size-4" />
								{/if}
								{client.label}
							</span>
						{/snippet}
					</CopyField>
				</div>
			{/each}
		</div>
	</div>
</ResponsiveDialog>

<ResponsiveDialog
	bind:this={viewSkillDialog}
	animate="slide"
	id="view-skill-dialog"
	title={viewedSkill?.displayName}
	class="md:w-3xl"
>
	{#if viewedSkillLoading}
		<Loading />
	{:else}
		<div class="nanobot view-skill-preview">
			<MarkdownEditor value={viewedSkillMarkdown} readonly />
		</div>
	{/if}
	{#if viewedSkill}
		<button
			class="btn btn-primary btn-sm mt-4 w-full"
			onclick={() => {
				selectedSkillToInstall = viewedSkill;
				viewSkillDialog?.close();
				installSkillDialog?.open();
			}}
		>
			Install
		</button>
	{/if}
</ResponsiveDialog>

<svelte:head>
	<title>Obot | Skills</title>
</svelte:head>
