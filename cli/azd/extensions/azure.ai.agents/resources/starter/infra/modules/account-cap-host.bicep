targetScope = 'resourceGroup'

@description('Name of the account on which to bootstrap the capability host. The account MUST exist in the resource group where this module is invoked (use a module-level scope to cross resource groups or subscriptions).')
param accountName string

@description('Network mode: none | managed | byo-vnet | byo-vnet-standard. Drives cap host name and properties.')
@allowed([
  'none'
  'managed'
  'byo-vnet'
  'byo-vnet-standard'
])
param networkMode string

@description('Resource ID of the agent subnet (delegated to Microsoft.App/environments). Required when networkMode is byo-vnet-standard (sets customerSubnet).')
param agentSubnetId string = ''

var isByoVnet = startsWith(networkMode, 'byo-vnet')
var isStandard = networkMode == 'byo-vnet-standard'
var isManaged = networkMode == 'managed'

resource account 'Microsoft.CognitiveServices/accounts@2025-06-01' existing = {
  name: accountName
}

// Non-Standard cap host (hosted-agent pattern, name='agents').
resource hostedAgentsCapHost 'Microsoft.CognitiveServices/accounts/capabilityHosts@2025-10-01-preview' = if (!isStandard) {
  parent: account
  name: 'agents'
  properties: {
    capabilityHostKind: 'Agents'
    enablePublicHostingEnvironment: !isByoVnet && !isManaged
  }
}

// Standard cap host (sample 15 pattern, name='caphostacct', customerSubnet).
resource standardAgentsCapHost 'Microsoft.CognitiveServices/accounts/capabilityHosts@2025-10-01-preview' = if (isStandard) {
  parent: account
  name: 'caphostacct'
  properties: {
    #disable-next-line BCP037
    capabilityHostKind: 'Agents'
    #disable-next-line BCP037
    customerSubnet: agentSubnetId
  }
}

output capabilityHostName string = isStandard ? 'caphostacct' : 'agents'
