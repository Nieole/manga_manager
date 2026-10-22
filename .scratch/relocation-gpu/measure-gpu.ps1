param([Parameter(Mandatory)][string]$Label, [int]$Seconds=10, [Parameter(Mandatory)][int]$BrowserProcessId, [Parameter(Mandatory)][string]$OutputPath)
$ErrorActionPreference='Stop'
$gpuProcesses=@(Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'chrome.exe' -and $_.CommandLine -match '--type=gpu-process' -and (!$BrowserProcessId -or $_.ParentProcessId -eq $BrowserProcessId) } | Select-Object -ExpandProperty ProcessId)
$values=@(Get-Counter '\GPU Engine(*)\Utilization Percentage' -SampleInterval 1 -MaxSamples $Seconds -ErrorAction SilentlyContinue | ForEach-Object {
  $sample=$_
  $total=0.0
  $validCounters=0
  foreach($counter in $sample.CounterSamples) {
    if($counter.InstanceName -match '^pid_(\d+)_.*_engtype_(?:high priority )?3d$' -and $gpuProcesses -contains [int]$Matches[1] -and $counter.Status -eq 0) { $validCounters++; $total += [Math]::Max([double]0,[double]$counter.CookedValue) }
  }
  if ($validCounters -gt 0) { [Math]::Round($total,2) }
})
if ($values.Count -lt [Math]::Max(3, $Seconds - 2)) { throw 'Insufficient valid browser GPU samples' }
$result=[ordered]@{label=$Label; metric='Chrome GPU process 3D engine utilization'; seconds=$Seconds; mean=[Math]::Round(($values|Measure-Object -Average).Average,2); maximum=($values|Measure-Object -Maximum).Maximum; samples=$values; utc=[DateTime]::UtcNow.ToString('o')}
$json=$result|ConvertTo-Json -Compress
Add-Content -LiteralPath $OutputPath -Value $json -Encoding utf8
$json
