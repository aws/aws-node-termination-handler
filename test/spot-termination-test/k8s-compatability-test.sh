#!/bin/bash

SCRIPTPATH="$( cd "$(dirname "$0")" ; pwd -P )"
versions=("1.15" "1.14" "1.13" "1.12" "1.11")
E_CODE=0

for i in "${!versions[@]}"; do 
   version=${versions[$i]}
   if [ $i -eq 0 ]; then
      $SCRIPTPATH/run-spot-termination-test.sh -i "test-$version" -v $version
      if [ $? -eq 0 ]; then 
         echo "✅ Passed test for K8s version $version"
      else 
         echo "❌ Failed test for K8s version $version"
         E_CODE=1
      fi
   else 
      $SCRIPTPATH/run-spot-termination-test.sh -i "test-$version" -v $version -n node-termination-handler:customtest -e ec2-meta-data-proxy:customtest
      if [ $? -eq 0 ]; then 
         echo "✅ Passed test for K8s version $version"
      else 
         echo "❌ Failed test for K8s version $version"
         E_CODE=1
      fi
   fi
done

exit $E_CODE
