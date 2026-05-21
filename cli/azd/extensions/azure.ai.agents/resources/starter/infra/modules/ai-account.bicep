targetScope = 'resourceGroup'

@description('Location for the new account')
param location string

@description('Tags for new resources')
param tags object = {}

@description('Name of the new AI Foundry (Cognitive Services) account. Empty triggers auto-generation using uniqueString(subscription().id, resourceGroup().id, location), matching the original template behavior. Ignored when useExistingAccount=true unless existingAccountResourceId is empty (fallback to in-RG existing-by-name).')
param accountName string = ''

@description('When true, the template will NOT create a new account; the existing account is referenced via existingAccountResourceId (preferred) or by accountName in the current resource group.')
param useExistingAccount bool = false

@description('Optional. Full ARM resource ID of an existing AI Foundry (Microsoft.CognitiveServices/accounts, kind=AIServices) account to reuse. Cross-RG/cross-subscription safe.')
param existingAccountResourceId string = ''

@description('Model deployments to create on the new account. Ignored for existing accounts.')
param deployments array = []

@description('Network mode: none | managed | byo-vnet | byo-vnet-standard')
@allowed([
  'none'
  'managed'
  'byo-vnet'
  'byo-vnet-standard'
])
param networkMode string = 'none'

@description('Resource ID of the agent subnet (delegated to Microsoft.App/environments). Required when networkMode starts with byo-vnet.')
param agentSubnetId string = ''

@description('Public IPv4 addresses or CIDRs allowed to reach the account data plane while public access is enabled. Used only when networkMode is byo-vnet or byo-vnet-standard.')
param clientIpAllowList array = []

@description('When true, set publicNetworkAccess to Disabled (fully lock down the account; requires running azd from inside the VNet). Only relevant when networkMode starts with byo-vnet.')
param disablePublicNetworkAccess bool = false

@description('Disable account-level local auth (account keys). Strongly recommended; default true.')
param disableLocalAuth bool = true

@description('Skip creating the account-scoped capability host. Defaults to true when useExistingAccount=true (assumes the existing account already has its cap host), and false otherwise. Set explicitly to bootstrap the account cap host on a BYO account for byo-vnet-standard.')
param skipAccountCapabilityHost bool = useExistingAccount

// ─────────────────────────────────────────────────────────────────────
// Derived values
//
// Note: BYO accounts must live in the SAME resource group as this deployment.
// We accept a full ARM ID for naming consistency (and to extract the account
// name), but we ignore the RG/subscription parts: the existing reference is
// scoped to the current RG. Document this constraint in the README.
// ─────────────────────────────────────────────────────────────────────

var existingAccountParts = split(existingAccountResourceId, '/')
var hasExistingResourceId = !empty(existingAccountResourceId)

// Backward-compatible auto-name: matches the original ai-project.bicep token
// (subscription.id + resourceGroup.id + location) so first-time deployments
// against the same env reuse the same account name.
var resourceToken = uniqueString(subscription().id, resourceGroup().id, location)
var resolvedNewAccountName = !empty(accountName) ? accountName : 'ai-account-${resourceToken}'
var resolvedExistingAccountName = hasExistingResourceId
  ? last(existingAccountParts)
  : (!empty(accountName) ? accountName : resolvedNewAccountName)

var isByoVnet = startsWith(networkMode, 'byo-vnet')
var isStandard = networkMode == 'byo-vnet-standard'
var isManaged = networkMode == 'managed'

var ipRules = [for ip in clientIpAllowList: { value: ip }]

// Public network access:
//   none / managed → Enabled (Foundry-managed isolation handles security for managed)
//   byo-vnet*      → Enabled with Deny ACL + IP allow-list, unless disablePublicNetworkAccess=true
var effectivePublicNetworkAccess = isByoVnet
  ? (disablePublicNetworkAccess ? 'Disabled' : 'Enabled')
  : 'Enabled'

// networkAcls: only meaningful for byo-vnet*; managed and none use the default Allow.
var effectiveNetworkAcls = isByoVnet
  ? {
      // Deny by default. AzureServices bypass is required so the Foundry control
      // plane (Microsoft-managed) can reach the account to orchestrate hosted-agent
      // image pulls from ACR (which must remain publicly reachable per Foundry
      // preview limitations). User-supplied IPs in clientIpAllowList are added
      // so that `azd deploy` / `azd ai agent invoke` work from the developer's
      // machine while public access stays Enabled.
      defaultAction: 'Deny'
      virtualNetworkRules: []
      ipRules: ipRules
      bypass: 'AzureServices'
    }
  : {
      defaultAction: 'Allow'
      virtualNetworkRules: []
      ipRules: []
    }

// networkInjections: omitted for none; managed uses useMicrosoftManagedNetwork=true; byo-vnet* uses the customer subnet.
var effectiveNetworkInjections = isManaged
  ? [
      {
        scenario: 'agent'
        subnetArmId: ''
        useMicrosoftManagedNetwork: true
      }
    ]
  : (isByoVnet
      ? [
          {
            scenario: 'agent'
            subnetArmId: agentSubnetId
            useMicrosoftManagedNetwork: false
          }
        ]
      : null)

// ─────────────────────────────────────────────────────────────────────
// New account
// ─────────────────────────────────────────────────────────────────────

#disable-next-line BCP036
resource newAccount 'Microsoft.CognitiveServices/accounts@2026-03-01' = if (!useExistingAccount) {
  name: resolvedNewAccountName
  location: location
  tags: tags
  sku: { name: 'S0' }
  kind: 'AIServices'
  identity: { type: 'SystemAssigned' }
  properties: {
    allowProjectManagement: true
    customSubDomainName: resolvedNewAccountName
    networkAcls: effectiveNetworkAcls
    publicNetworkAccess: effectivePublicNetworkAccess
    networkInjections: effectiveNetworkInjections
    disableLocalAuth: disableLocalAuth
  }

  @batchSize(1)
  resource modelDeployments 'deployments' = [for dep in deployments: {
    name: dep.name
    properties: { model: dep.model }
    sku: dep.sku
  }]
}

// ─────────────────────────────────────────────────────────────────────
// Existing account (cross-RG / cross-sub safe)
// ─────────────────────────────────────────────────────────────────────

resource existingAccount 'Microsoft.CognitiveServices/accounts@2026-03-01' existing = if (useExistingAccount) {
  name: resolvedExistingAccountName
}

// Create model deployments on the EXISTING account when the user picks an
// existing project/account but still wants the template to add a new model
// deployment (e.g. agent.yaml lists a model the existing account doesn't have).
// Mirrors the @batchSize(1) loop nested under newAccount above. Gated on
// useExistingAccount so the loop is skipped (and existingAccount unread) when
// we're in the new-account branch.
@batchSize(1)
resource existingAccountDeployments 'Microsoft.CognitiveServices/accounts/deployments@2026-03-01' = [for dep in deployments: if (useExistingAccount) {
  parent: existingAccount
  name: dep.name
  properties: { model: dep.model }
  sku: dep.sku
}]

// ─────────────────────────────────────────────────────────────────────
// Account-scoped capability host (optional)
// Sub-module makes the cap host creation easy to gate (only when the
// account exists at the time of deployment) and decouples it from the
// account branch.
// ─────────────────────────────────────────────────────────────────────

module accountCapHost './account-cap-host.bicep' = if (!skipAccountCapabilityHost) {
  name: 'account-cap-host-${resolvedNewAccountName}'
  params: {
    accountName: useExistingAccount ? resolvedExistingAccountName : resolvedNewAccountName
    networkMode: networkMode
    agentSubnetId: agentSubnetId
  }
  dependsOn: [
    newAccount
    existingAccount
  ]
}

// ─────────────────────────────────────────────────────────────────────
// Outputs (existing-vs-new short-circuit ternary; BCP318-suppressed)
// ─────────────────────────────────────────────────────────────────────

#disable-next-line BCP318
output accountId string = useExistingAccount ? existingAccount.id : newAccount.id
output accountName string = useExistingAccount ? resolvedExistingAccountName : resolvedNewAccountName
output accountResourceGroupName string = resourceGroup().name
output accountSubscriptionId string = subscription().subscriptionId
#disable-next-line BCP318
output accountPrincipalId string = useExistingAccount ? existingAccount.identity.principalId : newAccount.identity.principalId
#disable-next-line BCP318
output openAiEndpoint string = useExistingAccount ? existingAccount.properties.endpoints['OpenAI Language Model Instance API'] : newAccount.properties.endpoints['OpenAI Language Model Instance API']

// Account cap host name (whichever was created/exists); used by project cap host dependsOn.
output accountCapHostName string = isStandard ? 'caphostacct' : 'agents'
