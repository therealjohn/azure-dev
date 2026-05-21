targetScope = 'subscription'

@minLength(1)
@maxLength(64)
@description('Name of the environment that can be used as part of naming resource convention')
param environmentName string

@minLength(1)
@maxLength(90)
@description('Name of the resource group to use or create')
param resourceGroupName string = 'rg-${environmentName}'

@minLength(1)
@description('Primary location for all resources')
param location string

@description('Id of the user or app to assign application roles')
param principalId string

@description('Principal type of user or app')
param principalType string

// ─────────────────────────────────────────────────────────────────────
// Foundry account / project parameters
// ─────────────────────────────────────────────────────────────────────

@description('Optional. Name of the AI Account. If not provided, a new one will be created with an auto-generated name. When useExistingFoundryAccount is true, this is treated as the existing account name unless foundryAccountResourceId is set (in which case the name is parsed from the ARM ID).')
param foundryAccountName string = ''

@description('Optional. Full ARM resource ID of an existing AI Foundry account to reuse. The account must live in the SAME resource group as this deployment. When set, takes precedence over foundryAccountName for name extraction.')
param foundryAccountResourceId string = ''

@description('Name of the AI Foundry project')
param foundryProjectName string = 'ai-project-${environmentName}'

@description('When true, reference an existing Foundry project instead of creating one. Requires useExistingFoundryAccount=true (existing projects only live on existing accounts).')
param useExistingFoundryProject bool = false

@description('When true, reference an existing Foundry account instead of creating one. Implied true when foundryAccountResourceId is non-empty.')
param useExistingFoundryAccount bool = false

@description('Skip creating the account-scoped capability host. Defaults to true when useExistingFoundryAccount is true. Set false explicitly to bootstrap the cap host on a BYO account (typically needed for byo-vnet-standard).')
param skipAccountCapabilityHost bool = false

// ─────────────────────────────────────────────────────────────────────
// Network parameters
// ─────────────────────────────────────────────────────────────────────

@description('Network mode for the Foundry account: none (public, default) | managed (Microsoft-managed network) | byo-vnet (customer-delegated agent subnet + private endpoint) | byo-vnet-standard (byo-vnet + BYO Cosmos/Storage/AI Search with private endpoints).')
@allowed([
  'none'
  'managed'
  'byo-vnet'
  'byo-vnet-standard'
])
param foundryNetworkMode string = 'none'

@description('Optional. Full ARM resource ID of an existing VNet to reuse. The agent subnet must already be delegated to Microsoft.App/environments. Empty creates a new VNet in this resource group.')
param existingVnetResourceId string = ''

@description('Name of the new VNet. Ignored when existingVnetResourceId is set.')
param vnetName string = 'vnet-${environmentName}'

@description('VNet address prefix. Empty defaults to 192.168.0.0/16. Ignored for existing VNets.')
param vnetAddressPrefix string = ''

@description('Agent subnet name (delegated to Microsoft.App/environments).')
param agentSubnetName string = 'agent-subnet'

@description('Agent subnet prefix. Empty derives 192.168.0.0/24 from default VNet prefix. Ignored for existing VNets.')
param agentSubnetPrefix string = ''

@description('Private endpoint subnet name.')
param peSubnetName string = 'pe-subnet'

@description('PE subnet prefix. Empty derives 192.168.1.0/24. Ignored for existing VNets.')
param peSubnetPrefix string = ''

@description('JSON array of IPv4 addresses or CIDR ranges allowed to reach the Foundry account data plane while public access is enabled (only used for byo-vnet*).')
param clientIpAllowList array = []

@description('When true, set publicNetworkAccess=Disabled on the Foundry account (and on Standard data resources). Requires running azd from inside the VNet.')
param disablePublicNetworkAccess bool = false

// ─────────────────────────────────────────────────────────────────────
// BYO data resource parameters (byo-vnet-standard)
// ─────────────────────────────────────────────────────────────────────

@description('Optional. Full ARM resource ID of an existing Storage account to use as the Foundry project storageConnections target. Required services: Blob. Cross-RG / cross-sub safe.')
param existingStorageAccountResourceId string = ''

@description('Optional. Full ARM resource ID of an existing AI Search service. Cross-RG / cross-sub safe.')
param existingAiSearchResourceId string = ''

@description('Optional. Full ARM resource ID of an existing Cosmos DB account. Cross-RG / cross-sub safe.')
param existingCosmosDbAccountResourceId string = ''

@description('Map of existing private DNS zone FQDN -> resource group name. Empty value means create new in this RG.')
param existingDnsZones object = {}

@description('Subscription ID where existing private DNS zones live. Empty defaults to current subscription. Accepts bare GUID or /subscriptions/<guid> path.')
param dnsZonesSubscriptionId string = ''

// Extension-injected from azure.yaml service config
@description('Model deployments (JSON array from azure.yaml)')
param foundryProjectDeploymentsJson string = '[]'

@description('Connections (JSON array from azure.yaml)')
param foundryProjectConnectionsJson string = '[]'

@secure()
@description('Connection credentials (JSON map from azure.yaml)')
#disable-next-line secure-parameter-default
param foundryProjectConnectionCredentialsJson string = '{}'

// Existing resource detection (set by extension when reusing resources)
@description('Existing ACR connection name on the Foundry project. If set, ACR creation is skipped.')
param existingAcrConnectionName string = ''

@description('Existing ACR login server endpoint. Used as output when ACR creation is skipped.')
param existingContainerRegistryEndpoint string = ''

@description('Existing App Insights connection string (for existing projects)')
param existingApplicationInsightsConnectionString string = ''

@description('Existing App Insights resource ID (for existing projects)')
param existingApplicationInsightsResourceId string = ''

// ─────────────────────────────────────────────────────────────────────
// Provisioning toggles (opt-out env vars; match extension conventions)
// ─────────────────────────────────────────────────────────────────────

@description('Skip Azure Container Registry creation. Used by the azd ai agent extension when code-deploy mode is selected (the agent ZIP is uploaded directly, no ACR needed). Defaults false. See Azure/azure-dev PR #8242.')
param skipAcr bool = false

@description('Create the Foundry capability hosts (both account-scoped and project-scoped for Standard). Defaults true. The azd ai agent extension sets ENABLE_CAPABILITY_HOST=false when it manages the cap host itself via the Foundry control plane.')
param enableCapabilityHost bool = true

@description('Provision Application Insights + Log Analytics and connect them to a NEW project. Defaults true. Ignored for existing projects (which keep their existing monitoring wiring).')
param enableMonitoring bool = true

// ─────────────────────────────────────────────────────────────────────
// Derived values
// ─────────────────────────────────────────────────────────────────────

var tags = { 'azd-env-name': environmentName }
var createAcr = !skipAcr && empty(existingAcrConnectionName)
var resourceToken = uniqueString(subscription().id, resourceGroupName, location)

// useExistingFoundryAccount is implied true if the ARM ID was supplied.
var resolvedUseExistingFoundryAccount = useExistingFoundryAccount || !empty(foundryAccountResourceId)

// Account name: prefer parsed from ARM ID, else foundryAccountName, else
// empty (ai-account.bicep auto-generates with a deterministic token).
var accountNameFromId = !empty(foundryAccountResourceId) ? last(split(foundryAccountResourceId, '/')) : ''
var resolvedAccountName = !empty(accountNameFromId) ? accountNameFromId : foundryAccountName

// skipAccountCapabilityHost default: true when reusing existing account, false otherwise.
// User can override explicitly.
var resolvedSkipAccountCapHost = skipAccountCapabilityHost || (resolvedUseExistingFoundryAccount && foundryNetworkMode != 'byo-vnet-standard')

var isByoVnet = startsWith(foundryNetworkMode, 'byo-vnet')
var isStandard = foundryNetworkMode == 'byo-vnet-standard'

// Standard data connection names (deterministic per RG/location, like other modules use)
var storageConnectionName = 'storage-${resourceToken}'
var foundrySearchConnectionName = 'aisearch-${resourceToken}'
var cosmosConnectionName = 'cosmos-${resourceToken}'

resource rg 'Microsoft.Resources/resourceGroups@2025-04-01' = {
  name: resourceGroupName
  location: location
  tags: tags
}

// ─────────────────────────────────────────────────────────────────────
// VNet (gated by byo-vnet*)
// ─────────────────────────────────────────────────────────────────────

module vnet './modules/vnet.bicep' = if (isByoVnet) {
  scope: rg
  name: 'vnet'
  params: {
    location: location
    tags: tags
    vnetName: vnetName
    existingVnetResourceId: existingVnetResourceId
    vnetAddressPrefix: vnetAddressPrefix
    agentSubnetName: agentSubnetName
    agentSubnetPrefix: agentSubnetPrefix
    peSubnetName: peSubnetName
    peSubnetPrefix: peSubnetPrefix
  }
}

#disable-next-line BCP318
var agentSubnetIdValue = isByoVnet ? vnet.outputs.agentSubnetId : ''

// ─────────────────────────────────────────────────────────────────────
// Foundry account (new or existing) + account cap host
// ─────────────────────────────────────────────────────────────────────

module foundryAccount './modules/ai-account.bicep' = {
  scope: rg
  name: 'ai-account'
  params: {
    location: location
    tags: tags
    accountName: resolvedAccountName
    useExistingAccount: resolvedUseExistingFoundryAccount
    existingAccountResourceId: foundryAccountResourceId
    deployments: json(foundryProjectDeploymentsJson)
    networkMode: foundryNetworkMode
    agentSubnetId: agentSubnetIdValue
    clientIpAllowList: clientIpAllowList
    disablePublicNetworkAccess: disablePublicNetworkAccess
    skipAccountCapabilityHost: resolvedSkipAccountCapHost || !enableCapabilityHost
  }
}

// ─────────────────────────────────────────────────────────────────────
// Private endpoint + DNS for the Foundry account (gated by byo-vnet*)
// ─────────────────────────────────────────────────────────────────────

module accountPeDns './modules/private-endpoint-and-dns.bicep' = if (isByoVnet) {
  scope: rg
  name: 'account-pe-dns'
  params: {
    foundryAccountName: foundryAccount.outputs.accountName
    foundryAccountId: foundryAccount.outputs.accountId
    #disable-next-line BCP318
    vnetName: isByoVnet ? vnet.outputs.vnetName : ''
    #disable-next-line BCP318
    vnetSubscriptionId: isByoVnet ? vnet.outputs.vnetSubscriptionId : subscription().subscriptionId
    #disable-next-line BCP318
    vnetResourceGroupName: isByoVnet ? vnet.outputs.vnetResourceGroupName : rg.name
    peSubnetName: peSubnetName
    suffix: resourceToken
    existingDnsZones: existingDnsZones
    dnsZonesSubscriptionId: empty(dnsZonesSubscriptionId) ? subscription().subscriptionId : dnsZonesSubscriptionId
  }
}

// ─────────────────────────────────────────────────────────────────────
// AI project (new or existing) + monitoring + RBAC
// ─────────────────────────────────────────────────────────────────────

module foundryProject './modules/ai-project.bicep' = {
  scope: rg
  name: 'ai-project'
  params: {
    location: location
    tags: tags
    foundryProjectName: foundryProjectName
    foundryAccountName: foundryAccount.outputs.accountName
    connections: json(foundryProjectConnectionsJson)
    connectionCredentials: json(foundryProjectConnectionCredentialsJson)
    principalId: principalId
    principalType: principalType
    useExistingFoundryProject: useExistingFoundryProject
    enableMonitoring: enableMonitoring
    existingAppInsightsConnectionString: existingApplicationInsightsConnectionString
    existingAppInsightsResourceId: existingApplicationInsightsResourceId
    networkMode: foundryNetworkMode
  }
  dependsOn: [
    accountPeDns  // wait for PE/DNS (account must be reachable before project ops in BYO VNet)
  ]
}

// ─────────────────────────────────────────────────────────────────────
// Standard data resources (must come after the project so we have projectPrincipalId).
// Skipped for existing projects: existing projects with Standard are expected
// to already have data + connections + cap host wired up; running these
// modules would risk PUT conflicts on connections or cap host.
// ─────────────────────────────────────────────────────────────────────

var provisionStandard = isStandard && !useExistingFoundryProject

module storageStandard './modules/storage.bicep' = if (provisionStandard) {
  scope: rg
  name: 'storage-standard'
  params: {
    location: location
    tags: tags
    foundryAccountName: foundryAccount.outputs.accountName
    foundryProjectName: foundryProjectName
    projectPrincipalId: foundryProject.outputs.projectPrincipalId
    principalId: principalId
    principalType: principalType
    connectionName: storageConnectionName
    existingStorageAccountResourceId: existingStorageAccountResourceId
    disablePublicNetworkAccess: disablePublicNetworkAccess
  }
}

module foundrySearch './modules/ai-search.bicep' = if (provisionStandard) {
  scope: rg
  name: 'ai-search-standard'
  params: {
    location: location
    tags: tags
    foundryAccountName: foundryAccount.outputs.accountName
    foundryProjectName: foundryProjectName
    projectPrincipalId: foundryProject.outputs.projectPrincipalId
    principalId: principalId
    principalType: principalType
    connectionName: foundrySearchConnectionName
    existingSearchServiceResourceId: existingAiSearchResourceId
    disablePublicNetworkAccess: disablePublicNetworkAccess
  }
}

module cosmos './modules/cosmos.bicep' = if (provisionStandard) {
  scope: rg
  name: 'cosmos-standard'
  params: {
    location: location
    tags: tags
    foundryAccountName: foundryAccount.outputs.accountName
    foundryProjectName: foundryProjectName
    connectionName: cosmosConnectionName
    existingCosmosDbAccountResourceId: existingCosmosDbAccountResourceId
    disablePublicNetworkAccess: disablePublicNetworkAccess
  }
}

module dataPeDns './modules/private-endpoint-and-dns-data.bicep' = if (provisionStandard) {
  scope: rg
  name: 'data-pe-dns'
  params: {
    #disable-next-line BCP318
    vnetName: isByoVnet ? vnet.outputs.vnetName : ''
    #disable-next-line BCP318
    vnetSubscriptionId: isByoVnet ? vnet.outputs.vnetSubscriptionId : subscription().subscriptionId
    #disable-next-line BCP318
    vnetResourceGroupName: isByoVnet ? vnet.outputs.vnetResourceGroupName : rg.name
    peSubnetName: peSubnetName
    suffix: resourceToken
    #disable-next-line BCP318
    cosmosDbName: provisionStandard ? cosmos.outputs.accountName : ''
    #disable-next-line BCP318
    cosmosDbId: provisionStandard ? cosmos.outputs.accountId : ''
    #disable-next-line BCP318
    storageAccountName: provisionStandard ? storageStandard.outputs.accountName : ''
    #disable-next-line BCP318
    storageAccountId: provisionStandard ? storageStandard.outputs.accountId : ''
    #disable-next-line BCP318
    searchServiceName: provisionStandard ? foundrySearch.outputs.serviceName : ''
    #disable-next-line BCP318
    searchServiceId: provisionStandard ? foundrySearch.outputs.serviceId : ''
    existingDnsZones: existingDnsZones
    dnsZonesSubscriptionId: empty(dnsZonesSubscriptionId) ? subscription().subscriptionId : dnsZonesSubscriptionId
  }
}

// Pre-cap-host RBAC: Cosmos DB Operator on the project MI.
module cosmosRbacPre './modules/cosmos-rbac-pre.bicep' = if (provisionStandard) {
  scope: rg
  name: 'cosmos-rbac-pre'
  params: {
    #disable-next-line BCP318
    cosmosAccountName: provisionStandard ? cosmos.outputs.accountName : ''
    projectPrincipalId: foundryProject.outputs.projectPrincipalId
  }
}

// Project-scoped capability host (Standard only). Created AFTER project + all
// data connections + pre-cap-host RBAC + data PE/DNS. Honours enableCapabilityHost
// (extension sets ENABLE_CAPABILITY_HOST=false when it manages cap host itself).
module projectCapHost './modules/project-cap-host.bicep' = if (provisionStandard && enableCapabilityHost) {
  scope: rg
  name: 'project-cap-host'
  params: {
    foundryAccountName: foundryAccount.outputs.accountName
    foundryProjectName: foundryProject.outputs.projectName
    foundrySearchConnectionName: foundrySearchConnectionName
    storageConnectionName: storageConnectionName
    cosmosDbConnectionName: cosmosConnectionName
  }
  dependsOn: [
    storageStandard
    foundrySearch
    cosmos
    cosmosRbacPre
    dataPeDns
  ]
}

// Post-cap-host RBAC: Cosmos data role on /dbs/enterprise_memory, Storage Blob Data Owner with workspace condition.
// These depend on the project cap host being created.
module cosmosRbacPost './modules/cosmos-rbac-post.bicep' = if (provisionStandard) {
  scope: rg
  name: 'cosmos-rbac-post'
  params: {
    #disable-next-line BCP318
    cosmosAccountName: provisionStandard ? cosmos.outputs.accountName : ''
    projectPrincipalId: foundryProject.outputs.projectPrincipalId
    projectWorkspaceId: foundryProject.outputs.projectWorkspaceId
  }
  dependsOn: [
    projectCapHost
  ]
}

module storageRbacStandard './modules/storage-rbac-standard.bicep' = if (provisionStandard) {
  scope: rg
  name: 'storage-rbac-standard'
  params: {
    #disable-next-line BCP318
    storageAccountName: provisionStandard ? storageStandard.outputs.accountName : ''
    projectPrincipalId: foundryProject.outputs.projectPrincipalId
    workspaceId: foundryProject.outputs.projectWorkspaceId
  }
  dependsOn: [
    projectCapHost
  ]
}

// ─────────────────────────────────────────────────────────────────────
// ACR (existing condition)
// ─────────────────────────────────────────────────────────────────────

module acr './modules/acr.bicep' = if (createAcr) {
  scope: rg
  name: 'acr'
  params: {
    location: location
    tags: tags
    foundryAccountName: foundryAccount.outputs.accountName
    foundryProjectName: foundryProject.outputs.projectName
    projectPrincipalId: foundryProject.outputs.projectPrincipalId
    principalId: principalId
    principalType: principalType
    useExistingFoundryProject: useExistingFoundryProject
  }
}

// ═══════════════════════════════════════════════════════
// Outputs
// ═══════════════════════════════════════════════════════

// Resources
output AZURE_RESOURCE_GROUP string = resourceGroupName
output FOUNDRY_ACCOUNT_ID string = foundryAccount.outputs.accountId
output FOUNDRY_ACCOUNT_NAME string = foundryAccount.outputs.accountName
output FOUNDRY_PROJECT_NAME string = foundryProject.outputs.projectName

// Platform-injected variable names (match hosted agent runtime)
// See: https://learn.microsoft.com/azure/foundry/agents/how-to/deploy-hosted-agent#platform-injected-environment-variables
output FOUNDRY_PROJECT_ENDPOINT string = foundryProject.outputs.projectEndpoint
output FOUNDRY_PROJECT_ARM_ID string = foundryProject.outputs.projectId
output AZURE_OPENAI_ENDPOINT string = foundryAccount.outputs.openAiEndpoint

// Monitoring (already matches platform-injected name)
output APPLICATIONINSIGHTS_CONNECTION_STRING string = foundryProject.outputs.appInsightsConnectionString
output APPLICATIONINSIGHTS_RESOURCE_ID string = foundryProject.outputs.appInsightsResourceId

// Container Registry
#disable-next-line BCP318
output AZURE_CONTAINER_REGISTRY_ENDPOINT string = createAcr ? acr.outputs.loginServer : existingContainerRegistryEndpoint
#disable-next-line BCP318
output FOUNDRY_PROJECT_ACR_CONNECTION_NAME string = createAcr ? acr.outputs.connectionName : existingAcrConnectionName

// Connections (from azure.yaml)
output FOUNDRY_PROJECT_CONNECTION_IDS_JSON string = string(foundryProject.outputs.connectionIds)

// Network outputs
output FOUNDRY_NETWORK_MODE string = foundryNetworkMode
#disable-next-line BCP318
output AZURE_VNET_ID string = isByoVnet ? vnet.outputs.vnetId : ''
#disable-next-line BCP318
output AZURE_VNET_NAME string = isByoVnet ? vnet.outputs.vnetName : ''
#disable-next-line BCP318
output AZURE_AGENT_SUBNET_ID string = isByoVnet ? vnet.outputs.agentSubnetId : ''
#disable-next-line BCP318
output AZURE_PE_SUBNET_ID string = isByoVnet ? vnet.outputs.peSubnetId : ''

// Standard data resources
// Standard data resources (only emitted when isStandard AND a new project was provisioned).
#disable-next-line BCP318
output FOUNDRY_PROJECT_STORAGE_CONNECTION_NAME string = provisionStandard ? storageStandard.outputs.connectionName : ''
#disable-next-line BCP318
output FOUNDRY_PROJECT_AISEARCH_CONNECTION_NAME string = provisionStandard ? foundrySearch.outputs.connectionName : ''
#disable-next-line BCP318
output FOUNDRY_PROJECT_COSMOS_CONNECTION_NAME string = provisionStandard ? cosmos.outputs.connectionName : ''
