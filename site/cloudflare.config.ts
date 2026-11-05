import { defineConfig } from "cf/config";

export default defineConfig({
	accountId: "ad5ec479b9a421faa2ed06c3d1c2b23a",
	worker: {
		name: "stow-site",
		compatibilityDate: "2026-10-12",
		workersDev: false,
		assets: {
			notFoundHandling: "none",
		},
		domains: [
			"stow.chesterhillsolutions.ca",
		],
	},
});
