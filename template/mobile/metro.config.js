// Monorepo Metro config. Workspace packages ship TypeScript source; pnpm's hoisted linker
// (.npmrc node-linker=hoisted) puts every dependency in the root node_modules, so Metro
// watches the workspace root and resolves from both node_modules folders.
const path = require("node:path");
const { getDefaultConfig } = require("expo/metro-config");

const projectRoot = __dirname;
const workspaceRoot = path.resolve(projectRoot, "..");

/** @type {import('expo/metro-config').MetroConfig} */
const config = getDefaultConfig(projectRoot);

config.watchFolders = [...new Set([...(config.watchFolders ?? []), workspaceRoot])];
config.resolver.nodeModulesPaths = [path.resolve(projectRoot, "node_modules"), path.resolve(workspaceRoot, "node_modules")];

module.exports = config;
