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

@description('Optional. Name of the AI Account. If not provided, a new one will be created with an auto-generated name. When useExistingAccount is true, this is treated as the existing account name unless aiFoundryAccountResourceId is set (in which case the name is parsed from the ARM ID).')
param aiFoundryResourceName string = ''

@description('Optional. Full ARM resource ID of an existing AI Foundry account to reuse. The account must live in the SAME resource group as this deployment. When set, takes precedence over aiFoundryResourceName for name extraction.')
param aiFoundryAccountResourceId string = ''

@description('Name of the AI Foundry project')
param aiFoundryProjectName string = 'ai-project-${environmentName}'

@description('When true, reference an existing Foundry project instead of creating one. Requires useExistingAccount=true (existing projects only live on existing accounts).')
param useExistingAiProject bool = false

@description('When true, reference an existing Foundry account instead of creating one. Implied true when aiFoundryAccountResourceId is non-empty.')
param useExistingAiAccount bool = false

@description('Skip creating the account-scoped capability host. Defaults to true when useExistingAiAccount is true. Set false explicitly to bootstrap the cap host on a BYO account (typically needed for byo-vnet-standard).')
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
param aiFoundryNetworkMode string = 'none'

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
param aiProjectDeploymentsJson string = '[]'

@description('Connections (JSON array from azure.yaml)')
param aiProjectConnectionsJson string = '[]'

@secure()
@description('Connection credentials (JSON map from azure.yaml)')
#disable-next-line secure-parameter-default
param aiProjectConnectionCredentialsJson string = '{}'

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

// useExistingAiAccount is implied true if the ARM ID was supplied.
var resolvedUseExistingAiAccount = useExistingAiAccount || !empty(aiFoundryAccountResourceId)

// Account name: prefer parsed from ARM ID, else aiFoundryResourceName, else
// empty (ai-account.bicep auto-generates with a deterministic token).
var accountNameFromId = !empty(aiFoundryAccountResourceId) ? last(split(aiFoundryAccountResourceId, '/')) : ''
var resolvedAccountName = !empty(accountNameFromId) ? accountNameFromId : aiFoundryResourceName

// skipAccountCapabilityHost default: true when reusing existing account, false otherwise.
// User can override explicitly.
var resolvedSkipAccountCapHost = skipAccountCapabilityHost || (resolvedUseExistingAiAccount && aiFoundryNetworkMode != 'byo-vnet-standard')

var isByoVnet = startsWith(aiFoundryNetworkMode, 'byo-vnet')
var isStandard = aiFoundryNetworkMode == 'byo-vnet-standard'

// Standard data connection names (deterministic per RG/location, like other modules use)
var storageConnectionName = 'storage-${resourceToken}'
var aiSearchConnectionName = 'aisearch-${resourceToken}'
var cosmosConnectionName = 'cosmos-${resourceToken}'

resource rg 'Microsoft.Resources/resourceGroups@2021-04-01' = {
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

module aiAccount './modules/ai-account.bicep' = {
  scope: rg
  name: 'ai-account'
  params: {
    location: location
    tags: tags
    accountName: resolvedAccountName
    useExistingAccount: resolvedUseExistingAiAccount
    existingAccountResourceId: aiFoundryAccountResourceId
    deployments: json(aiProjectDeploymentsJson)
    networkMode: aiFoundryNetworkMode
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
    aiAccountName: aiAccount.outputs.accountName
    aiAccountId: aiAccount.outputs.accountId
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

module aiProject './modules/ai-project.bicep' = {
  scope: rg
  name: 'ai-project'
  params: {
    location: location
    tags: tags
    aiFoundryProjectName: aiFoundryProjectName
    aiAccountName: aiAccount.outputs.accountName
    connections: json(aiProjectConnectionsJson)
    connectionCredentials: json(aiProjectConnectionCredentialsJson)
    principalId: principalId
    principalType: principalType
    useExistingAiProject: useExistingAiProject
    enableMonitoring: enableMonitoring
    existingAppInsightsConnectionString: existingApplicationInsightsConnectionString
    existingAppInsightsResourceId: existingApplicationInsightsResourceId
    networkMode: aiFoundryNetworkMode
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

var provisionStandard = isStandard && !useExistingAiProject

module storageStandard './modules/storage.bicep' = if (provisionStandard) {
  scope: rg
  name: 'storage-standard'
  params: {
    location: location
    tags: tags
    aiAccountName: aiAccount.outputs.accountName
    aiProjectName: aiFoundryProjectName
    projectPrincipalId: aiProject.outputs.projectPrincipalId
    principalId: principalId
    principalType: principalType
    connectionName: storageConnectionName
    existingStorageAccountResourceId: existingStorageAccountResourceId
    disablePublicNetworkAccess: disablePublicNetworkAccess
  }
}

module aiSearch './modules/ai-search.bicep' = if (provisionStandard) {
  scope: rg
  name: 'ai-search-standard'
  params: {
    location: location
    tags: tags
    aiAccountName: aiAccount.outputs.accountName
    aiProjectName: aiFoundryProjectName
    projectPrincipalId: aiProject.outputs.projectPrincipalId
    principalId: principalId
    principalType: principalType
    connectionName: aiSearchConnectionName
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
    aiAccountName: aiAccount.outputs.accountName
    aiProjectName: aiFoundryProjectName
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
    searchServiceName: provisionStandard ? aiSearch.outputs.serviceName : ''
    #disable-next-line BCP318
    searchServiceId: provisionStandard ? aiSearch.outputs.serviceId : ''
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
    projectPrincipalId: aiProject.outputs.projectPrincipalId
  }
}

// Project-scoped capability host (Standard only). Created AFTER project + all
// data connections + pre-cap-host RBAC + data PE/DNS. Honours enableCapabilityHost
// (extension sets ENABLE_CAPABILITY_HOST=false when it manages cap host itself).
module projectCapHost './modules/project-cap-host.bicep' = if (provisionStandard && enableCapabilityHost) {
  scope: rg
  name: 'project-cap-host'
  params: {
    aiAccountName: aiAccount.outputs.accountName
    aiProjectName: aiProject.outputs.projectName
    aiSearchConnectionName: aiSearchConnectionName
    storageConnectionName: storageConnectionName
    cosmosDbConnectionName: cosmosConnectionName
  }
  dependsOn: [
    storageStandard
    aiSearch
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
    projectPrincipalId: aiProject.outputs.projectPrincipalId
    projectWorkspaceId: aiProject.outputs.projectWorkspaceId
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
    projectPrincipalId: aiProject.outputs.projectPrincipalId
    workspaceId: aiProject.outputs.projectWorkspaceId
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
    aiAccountName: aiAccount.outputs.accountName
    aiProjectName: aiProject.outputs.projectName
    projectPrincipalId: aiProject.outputs.projectPrincipalId
    principalId: principalId
    principalType: principalType
    useExistingAiProject: useExistingAiProject
  }
}

// ═══════════════════════════════════════════════════════
// Outputs
// ═══════════════════════════════════════════════════════

// Resources
output AZURE_RESOURCE_GROUP string = resourceGroupName
output AZURE_AI_ACCOUNT_ID string = aiAccount.outputs.accountId
output AZURE_AI_ACCOUNT_NAME string = aiAccount.outputs.accountName
output AZURE_AI_PROJECT_NAME string = aiProject.outputs.projectName

// Platform-injected variable names (match hosted agent runtime)
// See: https://learn.microsoft.com/azure/foundry/agents/how-to/deploy-hosted-agent#platform-injected-environment-variables
output FOUNDRY_PROJECT_ENDPOINT string = aiProject.outputs.projectEndpoint
output FOUNDRY_PROJECT_ARM_ID string = aiProject.outputs.projectId
output AZURE_OPENAI_ENDPOINT string = aiAccount.outputs.openAiEndpoint

// Monitoring (already matches platform-injected name)
output APPLICATIONINSIGHTS_CONNECTION_STRING string = aiProject.outputs.appInsightsConnectionString
output APPLICATIONINSIGHTS_RESOURCE_ID string = aiProject.outputs.appInsightsResourceId

// Container Registry
#disable-next-line BCP318
output AZURE_CONTAINER_REGISTRY_ENDPOINT string = createAcr ? acr.outputs.loginServer : existingContainerRegistryEndpoint
#disable-next-line BCP318
output AZURE_AI_PROJECT_ACR_CONNECTION_NAME string = createAcr ? acr.outputs.connectionName : existingAcrConnectionName

// Connections (from azure.yaml)
output AI_PROJECT_CONNECTION_IDS_JSON string = string(aiProject.outputs.connectionIds)

// Network outputs
output AZURE_AI_FOUNDRY_NETWORK_MODE string = aiFoundryNetworkMode
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
output AZURE_AI_PROJECT_STORAGE_CONNECTION_NAME string = provisionStandard ? storageStandard.outputs.connectionName : ''
#disable-next-line BCP318
output AZURE_AI_PROJECT_AISEARCH_CONNECTION_NAME string = provisionStandard ? aiSearch.outputs.connectionName : ''
#disable-next-line BCP318
output AZURE_AI_PROJECT_COSMOS_CONNECTION_NAME string = provisionStandard ? cosmos.outputs.connectionName : ''
